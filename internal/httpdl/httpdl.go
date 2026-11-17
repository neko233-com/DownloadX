// Package httpdl implements an NDM-style segmented HTTP(S) downloader.
package httpdl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/httpx"
)

type probeResult struct {
	finalURL     *url.URL
	size         int64
	ranges       bool
	etag         string
	lastModified string
	disposition  string
	contentType  string
}

type download struct {
	task    *engine.Task
	client  *http.Client
	dest    string
	probe   *probeResult
	file    *os.File
	segs    []*segment
	done    atomic.Int64
	limiter *engine.Limiter
}

// Download downloads t.URL to a destination chosen with engine.ResolveDest.
func Download(ctx context.Context, t *engine.Task) (*engine.Result, error) {
	client, err := httpx.NewClient(t)
	if err != nil {
		return nil, err
	}
	emit(t, "probing", 0, 0, 0, "")

	p, err := probe(ctx, t, client)
	if err != nil {
		return nil, err
	}

	name := engine.FilenameFromDisposition(p.disposition)
	if name == "" {
		name = engine.FilenameFromURL(p.finalURL.String())
	}
	dest, err := engine.ResolveDest(t, name)
	if err != nil {
		return nil, err
	}

	d := &download{
		task:    t,
		client:  client,
		dest:    dest,
		probe:   p,
		limiter: engine.NewLimiter(t.Limit),
	}
	return d.run(ctx)
}

func probe(ctx context.Context, t *engine.Task, client *http.Client) (*probeResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", "bytes=0-0")
	httpx.ApplyHeaders(req, t)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	p := &probeResult{
		finalURL:     resp.Request.URL,
		etag:         resp.Header.Get("ETag"),
		lastModified: resp.Header.Get("Last-Modified"),
		disposition:  resp.Header.Get("Content-Disposition"),
		contentType:  resp.Header.Get("Content-Type"),
	}

	switch resp.StatusCode {
	case http.StatusPartialContent:
		_, _, total, err := parseContentRange(resp.Header.Get("Content-Range"))
		if err != nil {
			return nil, fmt.Errorf("parse Content-Range: %w", err)
		}
		if total >= 0 {
			p.size = total
			p.ranges = true
		}
	case http.StatusOK:
		p.size = resp.ContentLength
	case http.StatusRequestedRangeNotSatisfiable:
		_, _, total, err := parseContentRange(resp.Header.Get("Content-Range"))
		if err == nil && total == 0 {
			p.size = 0
			p.ranges = true
			return p, nil
		}
		return nil, statusError(resp)
	default:
		return nil, statusError(resp)
	}
	return p, nil
}

func (d *download) run(ctx context.Context) (*engine.Result, error) {
	size := d.probe.size

	if size == 0 {
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
	if d.probe.ranges && !d.task.NoResume {
		if st, err := engine.LoadState(d.dest); err == nil && d.stateValid(st) {
			if st.Complete() {
				_ = engine.RemoveState(d.dest)
				emit(d.task, "done", size, size, len(st.Segments), "")
				return &engine.Result{Path: d.dest, Bytes: size}, nil
			}
			segs = segmentsFromState(st)
			resumed = true
		}
	}
	if segs == nil && d.probe.ranges {
		_ = engine.RemoveState(d.dest)
		segs = planSegments(size, d.task.ConnectionCount())
	}
	if segs == nil {
		return d.stream(ctx, size)
	}

	if err := f.Truncate(size); err != nil {
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
	go d.reportLoop(stopRep, repDone, size, len(segs))

	stopSave := make(chan struct{})
	saveDone := make(chan struct{})
	go d.saveLoop(stopSave, saveDone)

	err = d.downloadSegments(ctx)

	close(stopRep)
	<-repDone
	close(stopSave)
	<-saveDone

	if err != nil {
		if errors.Is(err, engine.ErrRangeIgnored) {
			_ = engine.RemoveState(d.dest)
			d.done.Store(0)
			return d.stream(ctx, size)
		}
		d.saveState()
		return nil, err
	}

	_ = engine.RemoveState(d.dest)
	emit(d.task, "done", size, size, len(segs), "")
	return &engine.Result{Path: d.dest, Bytes: size}, nil
}

func (d *download) stream(ctx context.Context, knownSize int64) (*engine.Result, error) {
	_ = engine.RemoveState(d.dest)
	if err := d.file.Truncate(0); err != nil {
		return nil, err
	}
	// A marker state keeps the same destination name across restarts even
	// though a Range-less server cannot resume; the next run starts over.
	_ = engine.SaveState(d.dest, &engine.State{URL: d.probe.finalURL.String(), Size: knownSize})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.probe.finalURL.String(), nil)
	if err != nil {
		return nil, err
	}
	httpx.ApplyHeaders(req, d.task)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, statusError(resp)
	}

	total := resp.ContentLength
	if total <= 0 {
		total = knownSize
	}
	if total < 0 {
		total = 0
	}

	d.done.Store(0)
	stop := make(chan struct{})
	repDone := make(chan struct{})
	go d.reportLoop(stop, repDone, total, 1)

	var offset int64
	buf := make([]byte, 32*1024)
	var copyErr error
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if d.limiter != nil {
				if werr := d.limiter.WaitN(ctx, n); werr != nil {
					copyErr = werr
					break
				}
			}
			if _, werr := d.file.WriteAt(buf[:n], offset); werr != nil {
				copyErr = werr
				break
			}
			offset += int64(n)
			d.done.Store(offset)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			copyErr = rerr
			break
		}
	}

	close(stop)
	<-repDone

	if copyErr == nil && ctx.Err() != nil {
		copyErr = ctx.Err()
	}
	if copyErr == nil && total > 0 && offset != total {
		copyErr = fmt.Errorf("short read: got %d of %d bytes", offset, total)
	}
	if copyErr != nil {
		return nil, copyErr
	}

	_ = engine.RemoveState(d.dest)
	emit(d.task, "done", offset, offset, 1, "")
	return &engine.Result{Path: d.dest, Bytes: offset}, nil
}

func (d *download) stateValid(st *engine.State) bool {
	if st.Size != d.probe.size || len(st.Segments) == 0 {
		return false
	}
	if st.URL != d.probe.finalURL.String() {
		return false
	}
	if st.ETag != "" && d.probe.etag != "" && st.ETag != d.probe.etag {
		return false
	}
	if st.LastModified != "" && d.probe.lastModified != "" && st.LastModified != d.probe.lastModified {
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

func (d *download) saveState() {
	st := &engine.State{
		URL:          d.probe.finalURL.String(),
		Size:         d.probe.size,
		ETag:         d.probe.etag,
		LastModified: d.probe.lastModified,
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

func (d *download) reportLoop(stop <-chan struct{}, done chan<- struct{}, total int64, conns int) {
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

func (d *download) saveLoop(stop <-chan struct{}, done chan<- struct{}) {
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

// httpError is a non-2xx HTTP response. 4xx responses (except 408/429) are
// permanent and are not retried.
type httpError struct {
	code    int
	status  string
	snippet string
	fatal   bool
}

func (e *httpError) Error() string {
	if e.snippet == "" {
		return "http " + e.status
	}
	return fmt.Sprintf("http %s: %s", e.status, e.snippet)
}

func statusError(resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	code := resp.StatusCode
	return &httpError{
		code:    code,
		status:  resp.Status,
		snippet: strings.TrimSpace(string(snippet)),
		fatal:   code >= 400 && code < 500 && code != http.StatusRequestTimeout && code != http.StatusTooManyRequests,
	}
}

// parseContentRange parses "bytes start-end/total" and "bytes */total".
// total is -1 when the server reports "*".
func parseContentRange(v string) (start, end, total int64, err error) {
	v = strings.TrimSpace(v)
	if len(v) < 6 || !strings.EqualFold(v[:6], "bytes ") {
		return 0, 0, 0, fmt.Errorf("malformed Content-Range %q", v)
	}
	v = strings.TrimSpace(v[6:])
	rng, tot, ok := strings.Cut(v, "/")
	if !ok {
		return 0, 0, 0, fmt.Errorf("malformed Content-Range %q", v)
	}
	rng = strings.TrimSpace(rng)
	if tot == "*" {
		total = -1
	} else {
		total, err = strconv.ParseInt(strings.TrimSpace(tot), 10, 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("malformed Content-Range total: %w", err)
		}
	}
	if rng == "*" {
		return -1, -1, total, nil
	}
	s, e, ok := strings.Cut(rng, "-")
	if !ok {
		return 0, 0, 0, fmt.Errorf("malformed Content-Range range %q", rng)
	}
	start, err = strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("malformed Content-Range start: %w", err)
	}
	end, err = strconv.ParseInt(strings.TrimSpace(e), 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("malformed Content-Range end: %w", err)
	}
	return start, end, total, nil
}
