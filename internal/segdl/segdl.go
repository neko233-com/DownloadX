// Package segdl downloads independent parts concurrently, keeps completed
// parts on disk, and concatenates them in order.
package segdl

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Job produces one part. Fetch is called at most once per successful run.
type Job struct {
	Fetch func(ctx context.Context) ([]byte, error)
}

// ProgressFunc reports completed items and total downloaded bytes.
type ProgressFunc func(done, total int, bytes int64)

// Run downloads all jobs into dir, reusing existing part files. It returns
// the ordered list of part paths.
func Run(ctx context.Context, jobs []Job, dir string, conns int, progress ProgressFunc) ([]string, error) {
	if len(jobs) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if conns < 1 {
		conns = 1
	}
	if conns > len(jobs) {
		conns = len(jobs)
	}

	paths := make([]string, len(jobs))
	for i := range jobs {
		paths[i] = filepath.Join(dir, fmt.Sprintf("%06d.part", i))
	}

	work := make(chan int, len(jobs))
	for i := range jobs {
		work <- i
	}
	close(work)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		firstErr  error
		doneItems int
		doneBytes int64
	)
	total := len(jobs)

	report := func(size int64) {
		mu.Lock()
		doneItems++
		doneBytes += size
		d, b := doneItems, doneBytes
		mu.Unlock()
		if progress != nil {
			progress(d, total, b)
		}
	}

	for w := 0; w < conns; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				if ctx.Err() != nil {
					return
				}
				path := paths[idx]
				if fi, err := os.Stat(path); err == nil {
					report(fi.Size())
					continue
				}
				data, err := jobs[idx].Fetch(ctx)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					cancel()
					return
				}
				if err := writePart(path, data); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					cancel()
					return
				}
				report(int64(len(data)))
			}
		}()
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return paths, nil
}

func writePart(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Concat writes files to dest in order, atomically replacing dest on success.
func Concat(files []string, dest string) error {
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = out.Close()
			_ = os.Remove(tmp)
		}
	}()

	for _, name := range files {
		in, err := os.Open(name)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	ok = true
	return nil
}
