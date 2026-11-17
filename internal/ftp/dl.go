package ftp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
)

type segment struct {
	start int64
	end   int64
	done  atomic.Int64
}

func (s *segment) size() int64 { return s.end - s.start + 1 }

type ftpDownload struct {
	task       *engine.Task
	opts       Options
	addr       string
	remotePath string
	dest       string
	size       int64
	mdtm       string
	file       *os.File
	segs       []*segment
	done       atomic.Int64
	limiter    *engine.Limiter
}

// Download downloads an ftp:// or ftps:// URL.
func Download(ctx context.Context, t *engine.Task) (*engine.Result, error) {
	u, err := url.Parse(t.URL)
	if err != nil {
		return nil, fmt.Errorf("parse ftp url: %w", err)
	}
	if u.Scheme != "ftp" && u.Scheme != "ftps" {
		return nil, fmt.Errorf("unsupported ftp scheme %q", u.Scheme)
	}

	opts := Options{
		User:     t.Username,
		Pass:     t.Password,
		TLS:      u.Scheme == "ftps",
		Insecure: t.Insecure,
		Timeout:  t.Timeout,
	}
	if u.User != nil {
		opts.User = u.User.Username()
		opts.Pass, _ = u.User.Password()
	}
	addr := u.Host
	if u.Port() == "" {
		addr = net.JoinHostPort(u.Hostname(), "21")
	}

	emit(t, "probing", 0, 0, 0, "")
	sess, err := Dial(ctx, addr, opts)
	if err != nil {
		return nil, err
	}
	if err := sess.Login(); err != nil {
		sess.Close()
		return nil, err
	}
	if err := sess.SetBinary(); err != nil {
		sess.Close()
		return nil, err
	}

	size, sizeErr := sess.Size(u.Path)
	if sizeErr != nil {
		size = -1
	}
	mdtm := ""
	if mt, err := sess.ModTime(u.Path); err == nil {
		mdtm = mt.UTC().Format(time.RFC3339)
	}
	sess.Close()

	name := engine.FilenameFromURL(t.URL)
	dest, err := engine.ResolveDest(t, name)
	if err != nil {
		return nil, err
	}

	d := &ftpDownload{
		task:       t,
		opts:       opts,
		addr:       addr,
		remotePath: u.Path,
		dest:       dest,
		size:       size,
		mdtm:       mdtm,
		limiter:    engine.NewLimiter(t.Limit),
	}
	return d.run(ctx)
}

func (d *ftpDownload) run(ctx context.Context) (*engine.Result, error) {
	if d.size == 0 {
		if err := os.WriteFile(d.dest, nil, 0o644); err != nil {
			return nil, err
		}
		_ = engine.RemoveState(d.dest)
		emit(d.task, "done", 0, 0, 0, "")
		return &engine.Result{Path: d.dest}, nil
	}

	f, err := os.OpenFile(d.dest, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	d.file = f
	defer f.Close()

	var segs []*segment
	resumed := false
	if d.size > 0 && !d.task.NoResume {
		if st, err := engine.LoadState(d.dest); err == nil && d.stateValid(st) {
			if st.Complete() {
				_ = engine.RemoveState(d.dest)
				emit(d.task, "done", d.size, d.size, len(st.Segments), "")
				return &engine.Result{Path: d.dest, Bytes: d.size}, nil
			}
			segs = segmentsFromState(st)
			resumed = true
		}
	}
	if segs == nil && d.size > 0 {
		_ = engine.RemoveState(d.dest)
		for _, p := range engine.PlanSegments(d.size, d.task.ConnectionCount()) {
			segs = append(segs, &segment{start: p.Start, end: p.End})
		}
	}
	if segs == nil {
		return d.stream(ctx)
	}

	if err := f.Truncate(d.size); err != nil {
		return nil, err
	}
	d.segs = segs
	if resumed {
		var done int64
		for _, s := range segs {
			done += s.done.Load()
		}
		d.done.Store(done)
	}
	d.saveState()

	stopRep := make(chan struct{})
	repDone := make(chan struct{})
	go d.reportLoop(stopRep, repDone, d.size, len(segs))

	stopSave := make(chan struct{})
	saveDone := make(chan struct{})
	go d.saveLoop(stopSave, saveDone)

	err = d.downloadSegments(ctx)

	close(stopRep)
	<-repDone
	close(stopSave)
	<-saveDone

	if err != nil {
		if errors.Is(err, ErrNoREST) {
			_ = engine.RemoveState(d.dest)
			d.done.Store(0)
			return d.stream(ctx)
		}
		d.saveState()
		return nil, err
	}

	_ = engine.RemoveState(d.dest)
	emit(d.task, "done", d.size, d.size, len(segs), "")
	return &engine.Result{Path: d.dest, Bytes: d.size}, nil
}

func (d *ftpDownload) stream(ctx context.Context) (*engine.Result, error) {
	if err := d.file.Truncate(0); err != nil {
		return nil, err
	}
	_ = engine.RemoveState(d.dest)
	_ = engine.SaveState(d.dest, &engine.State{URL: d.task.URL, Size: d.size})

	sess, err := d.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	reader, err := sess.OpenRanged(ctx, d.remotePath, 0)
	if err != nil {
		return nil, err
	}

	total := d.size
	if total < 0 {
		total = 0
	}
	d.done.Store(0)
	stop := make(chan struct{})
	repDone := make(chan struct{})
	go d.reportLoop(stop, repDone, total, 1)

	copyErr := d.copyStream(ctx, reader)
	closeErr := reader.Close()

	close(stop)
	<-repDone

	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil && ctx.Err() != nil {
		copyErr = ctx.Err()
	}
	if copyErr != nil {
		return nil, copyErr
	}

	_ = engine.RemoveState(d.dest)
	n := d.done.Load()
	emit(d.task, "done", n, n, 1, "")
	return &engine.Result{Path: d.dest, Bytes: n}, nil
}

func (d *ftpDownload) copyStream(ctx context.Context, r io.Reader) error {
	var offset int64
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if d.limiter != nil {
				if werr := d.limiter.WaitN(ctx, n); werr != nil {
					return werr
				}
			}
			if _, werr := d.file.WriteAt(buf[:n], offset); werr != nil {
				return werr
			}
			offset += int64(n)
			d.done.Store(offset)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (d *ftpDownload) downloadSegments(ctx context.Context) error {
	ctxSeg, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for _, s := range d.segs {
		if s.done.Load() >= s.size() {
			continue
		}
		wg.Add(1)
		go func(s *segment) {
			defer wg.Done()
			if err := d.worker(ctxSeg, s); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(s)
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return err
	}
	return firstErr
}

func (d *ftpDownload) worker(ctx context.Context, s *segment) error {
	var sess *Session
	defer func() {
		if sess != nil {
			_ = sess.Close()
		}
	}()

	var lastErr error
	for attempt := 0; ; attempt++ {
		if sess == nil {
			var err error
			sess, err = d.dial(ctx)
			if err != nil {
				lastErr = err
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if attempt >= d.task.Retries {
					return fmt.Errorf("ftp connect: %w", lastErr)
				}
				if err := sleepBackoff(ctx, attempt); err != nil {
					return err
				}
				continue
			}
		}

		err := d.fetchSegment(ctx, sess, s)
		if err == nil {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = sess.Close()
		sess = nil

		if errors.Is(err, ErrNoREST) || isFatalFTP(err) {
			return err
		}
		if attempt >= d.task.Retries {
			return fmt.Errorf("segment %d-%d: %w", s.start, s.end, lastErr)
		}
		if err := sleepBackoff(ctx, attempt); err != nil {
			return err
		}
	}
}

func (d *ftpDownload) fetchSegment(ctx context.Context, sess *Session, s *segment) error {
	done := s.done.Load()
	if done >= s.size() {
		return nil
	}
	offset := s.start + done
	reader, err := sess.OpenRanged(ctx, d.remotePath, offset)
	if err != nil {
		return err
	}
	copyErr := d.copyRange(ctx, reader, offset, s)
	closeErr := reader.Close()

	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if s.done.Load() < s.size() {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (d *ftpDownload) copyRange(ctx context.Context, r io.Reader, offset int64, s *segment) error {
	remaining := s.size() - s.done.Load()
	buf := make([]byte, 32*1024)
	for remaining > 0 {
		n, err := r.Read(buf)
		if n > 0 {
			if d.limiter != nil {
				if werr := d.limiter.WaitN(ctx, n); werr != nil {
					return werr
				}
			}
			if _, werr := d.file.WriteAt(buf[:n], offset); werr != nil {
				return werr
			}
			offset += int64(n)
			remaining -= int64(n)
			s.done.Add(int64(n))
			d.done.Add(int64(n))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if remaining > 0 {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (d *ftpDownload) dial(ctx context.Context) (*Session, error) {
	sess, err := Dial(ctx, d.addr, d.opts)
	if err != nil {
		return nil, err
	}
	if err := sess.Login(); err != nil {
		sess.Close()
		return nil, err
	}
	if err := sess.SetBinary(); err != nil {
		sess.Close()
		return nil, err
	}
	return sess, nil
}

func (d *ftpDownload) stateValid(st *engine.State) bool {
	if st.URL != d.task.URL || st.Size != d.size || len(st.Segments) == 0 {
		return false
	}
	if st.LastModified != "" && d.mdtm != "" && st.LastModified != d.mdtm {
		return false
	}
	info, err := os.Stat(d.dest)
	if err != nil || info.Size() != st.Size {
		return false
	}
	var offset int64
	for _, seg := range st.Segments {
		if seg.Start != offset || seg.End < seg.Start || seg.Done < 0 || seg.Done > seg.Len() {
			return false
		}
		offset = seg.End + 1
	}
	return offset == st.Size
}

func segmentsFromState(st *engine.State) []*segment {
	segs := make([]*segment, 0, len(st.Segments))
	for _, s := range st.Segments {
		seg := &segment{start: s.Start, end: s.End}
		seg.done.Store(s.Done)
		segs = append(segs, seg)
	}
	return segs
}

func (d *ftpDownload) saveState() {
	st := &engine.State{
		URL:          d.task.URL,
		Size:         d.size,
		LastModified: d.mdtm,
	}
	for _, s := range d.segs {
		st.Segments = append(st.Segments, engine.Segment{
			Start: s.start,
			End:   s.end,
			Done:  s.done.Load(),
		})
	}
	_ = engine.SaveState(d.dest, st)
}

func (d *ftpDownload) reportLoop(stop <-chan struct{}, done chan<- struct{}, total int64, conns int) {
	defer close(done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			emit(d.task, "downloading", d.done.Load(), total, conns, "")
			return
		case <-ticker.C:
			emit(d.task, "downloading", d.done.Load(), total, conns, "")
		}
	}
}

func (d *ftpDownload) saveLoop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			d.saveState()
			return
		case <-ticker.C:
			d.saveState()
		}
	}
}

func isFatalFTP(err error) bool {
	var fe *Error
	if !errors.As(err, &fe) {
		return false
	}
	switch fe.Code {
	case 500, 501, 502, 503, 504, 530, 532, 550, 551, 552, 553:
		return true
	}
	return false
}

func sleepBackoff(ctx context.Context, attempt int) error {
	wait := time.Duration(1<<uint(min(attempt, 4))) * 500 * time.Millisecond
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func emit(t *engine.Task, phase string, done, total int64, conns int, msg string) {
	if t.Progress == nil {
		return
	}
	t.Progress(engine.Progress{
		Phase:       phase,
		Done:        done,
		Total:       total,
		Connections: conns,
		Message:     msg,
	})
}
