package httpdl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/httpx"
)

type segment struct {
	start int64
	end   int64
	done  atomic.Int64
}

func (s *segment) size() int64 { return s.end - s.start + 1 }

func planSegments(size int64, conns int) []*segment {
	parts := engine.PlanSegments(size, conns)
	segs := make([]*segment, 0, len(parts))
	for _, p := range parts {
		segs = append(segs, &segment{start: p.Start, end: p.End})
	}
	return segs
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

func (d *download) downloadSegments(ctx context.Context) error {
	ctxSeg, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		firstErr     error
		rangeIgnored atomic.Bool
	)

	for _, s := range d.segs {
		if s.done.Load() >= s.size() {
			continue
		}
		wg.Add(1)
		go func(s *segment) {
			defer wg.Done()
			err := d.worker(ctxSeg, s)
			if err == nil {
				return
			}
			if errors.Is(err, engine.ErrRangeIgnored) {
				rangeIgnored.Store(true)
				cancel()
				return
			}
			mu.Lock()
			if firstErr == nil {
				firstErr = err
			}
			mu.Unlock()
		}(s)
	}
	wg.Wait()

	if rangeIgnored.Load() {
		return engine.ErrRangeIgnored
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if firstErr != nil {
		return firstErr
	}
	return nil
}

func (d *download) worker(ctx context.Context, s *segment) error {
	for attempt := 0; ; attempt++ {
		err := d.fetchSegment(ctx, s)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var he *httpError
		if errors.As(err, &he) && he.fatal {
			return err
		}
		if attempt >= d.task.Retries {
			return fmt.Errorf("segment %d-%d: %w", s.start, s.end, err)
		}
		wait := time.Duration(1<<uint(min(attempt, 4))) * 500 * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (d *download) fetchSegment(ctx context.Context, s *segment) error {
	for {
		done := s.done.Load()
		if done >= s.size() {
			return nil
		}
		offset := s.start + done

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.probe.finalURL.String(), nil)
		if err != nil {
			return err
		}
		httpx.ApplyHeaders(req, d.task)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, s.end))

		resp, err := d.client.Do(req)
		if err != nil {
			return err
		}

		switch resp.StatusCode {
		case http.StatusPartialContent:
			start, _, _, err := parseContentRange(resp.Header.Get("Content-Range"))
			if err != nil {
				resp.Body.Close()
				return err
			}
			if start != offset {
				resp.Body.Close()
				return fmt.Errorf("server returned range starting at %d, want %d", start, offset)
			}
			err = d.copyRange(ctx, resp.Body, offset, s)
			resp.Body.Close()
			if err != nil {
				return err
			}
		case http.StatusOK:
			resp.Body.Close()
			return engine.ErrRangeIgnored
		case http.StatusRequestedRangeNotSatisfiable:
			resp.Body.Close()
			if s.done.Load() >= s.size() {
				return nil
			}
			return &httpError{code: resp.StatusCode, status: resp.Status, fatal: true}
		default:
			err := statusError(resp)
			resp.Body.Close()
			return err
		}
	}
}

func (d *download) copyRange(ctx context.Context, body io.Reader, offset int64, s *segment) error {
	remaining := s.size() - s.done.Load()
	buf := make([]byte, 32*1024)
	for remaining > 0 {
		n, rerr := body.Read(buf)
		if n > 0 {
			if d.limiter != nil {
				if err := d.limiter.WaitN(ctx, n); err != nil {
					return err
				}
			}
			if _, err := d.file.WriteAt(buf[:n], offset); err != nil {
				return err
			}
			offset += int64(n)
			remaining -= int64(n)
			s.done.Add(int64(n))
			d.done.Add(int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if remaining > 0 {
		return io.ErrUnexpectedEOF
	}
	return nil
}
