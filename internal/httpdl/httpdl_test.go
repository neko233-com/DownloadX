package httpdl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
)

type rangeServer struct {
	data             []byte
	ranges           bool
	ignoreRangeAfter int64 // request count after which ranges are ignored
	status           int
	etag             string
	lastModified     string
	disposition      string
	delay            time.Duration
	requireHeaders   map[string]string
	requireCookie    string
	requireUser      string
	requirePass      string

	requests    atomic.Int64
	mu          sync.Mutex
	rangeStarts []int64
}

func (h *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := h.requests.Add(1)

	if h.requireUser != "" {
		user, pass, ok := r.BasicAuth()
		if !ok || user != h.requireUser || pass != h.requirePass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	for k, v := range h.requireHeaders {
		if r.Header.Get(k) != v {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "missing header %s", k)
			return
		}
	}
	if h.requireCookie != "" && r.Header.Get("Cookie") != h.requireCookie {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "missing cookie")
		return
	}
	if h.status != 0 {
		w.WriteHeader(h.status)
		fmt.Fprintf(w, "status %d", h.status)
		return
	}
	if h.etag != "" {
		w.Header().Set("ETag", h.etag)
	}
	if h.lastModified != "" {
		w.Header().Set("Last-Modified", h.lastModified)
	}
	if h.disposition != "" {
		w.Header().Set("Content-Disposition", h.disposition)
	}

	rng := r.Header.Get("Range")
	useRange := h.ranges && rng != "" && (h.ignoreRangeAfter == 0 || n <= h.ignoreRangeAfter)
	if useRange {
		start, end, ok := parseTestRange(rng)
		if !ok || start >= int64(len(h.data)) {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(h.data)))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= int64(len(h.data)) {
			end = int64(len(h.data)) - 1
		}
		h.mu.Lock()
		h.rangeStarts = append(h.rangeStarts, start)
		h.mu.Unlock()

		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(h.data)))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		h.write(w, h.data[start:end+1])
		return
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(h.data)))
	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(http.StatusOK)
	h.write(w, h.data)
}

func (h *rangeServer) write(w io.Writer, b []byte) {
	if h.delay <= 0 {
		_, _ = w.Write(b)
		return
	}
	const chunk = 16 * 1024
	for len(b) > 0 {
		n := min(chunk, len(b))
		if _, err := w.Write(b[:n]); err != nil {
			return
		}
		b = b[n:]
		time.Sleep(h.delay)
	}
}

func (h *rangeServer) starts() []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]int64(nil), h.rangeStarts...)
}

func parseTestRange(v string) (int64, int64, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(v, "bytes="))
	a, b, ok := strings.Cut(v, "-")
	if !ok {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(a, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end, err := strconv.ParseInt(b, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return start, end, true
}

func testData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + i/7)
	}
	return b
}

func newTask(srv *httptest.Server, dir string, conns int) *engine.Task {
	return &engine.Task{
		URL:         srv.URL + "/file.bin",
		Dir:         dir,
		Connections: conns,
		Retries:     1,
		Timeout:     5 * time.Second,
	}
}

func TestSegmentedDownload(t *testing.T) {
	data := testData(4 << 20)
	h := &rangeServer{data: data, ranges: true}
	srv := httptest.NewServer(h)
	defer srv.Close()

	var (
		mu   sync.Mutex
		last engine.Progress
	)
	task := newTask(srv, t.TempDir(), 8)
	task.Progress = func(p engine.Progress) {
		mu.Lock()
		last = p
		mu.Unlock()
	}

	res, err := Download(context.Background(), task)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch: got %d bytes", len(got))
	}
	if res.Bytes != int64(len(data)) {
		t.Fatalf("Bytes = %d, want %d", res.Bytes, len(data))
	}
	if got := h.requests.Load(); got < 9 {
		t.Errorf("requests = %d, want at least 9 (probe + 8 segments)", got)
	}
	if _, err := os.Stat(engine.StatePath(res.Path)); !os.IsNotExist(err) {
		t.Errorf("state file still present: %v", err)
	}
	mu.Lock()
	final := last
	mu.Unlock()
	if final.Phase != "done" || final.Done != int64(len(data)) || final.Total != int64(len(data)) {
		t.Errorf("final progress = %+v", final)
	}
}

func TestSmallFileSingleSegment(t *testing.T) {
	data := testData(1000)
	h := &rangeServer{data: data, ranges: true}
	srv := httptest.NewServer(h)
	defer srv.Close()

	res, err := Download(context.Background(), newTask(srv, t.TempDir(), 16))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
	if got := h.requests.Load(); got != 2 {
		t.Errorf("requests = %d, want 2", got)
	}
}

func TestNoRangeFallbackToStream(t *testing.T) {
	data := testData(256 << 10)
	h := &rangeServer{data: data, ranges: false}
	srv := httptest.NewServer(h)
	defer srv.Close()

	res, err := Download(context.Background(), newTask(srv, t.TempDir(), 8))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
	if _, err := os.Stat(engine.StatePath(res.Path)); !os.IsNotExist(err) {
		t.Errorf("state file still present: %v", err)
	}
}

func TestUnknownTotalStreams(t *testing.T) {
	data := testData(128 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 0-0/*")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[:1])
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	res, err := Download(context.Background(), newTask(srv, t.TempDir(), 8))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch: got %d bytes", len(got))
	}
}

func TestRangeIgnoredMidTransfer(t *testing.T) {
	data := testData(2 << 20)
	h := &rangeServer{data: data, ranges: true, ignoreRangeAfter: 1}
	srv := httptest.NewServer(h)
	defer srv.Close()

	res, err := Download(context.Background(), newTask(srv, t.TempDir(), 8))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch after fallback: got %d bytes, want %d", len(got), len(data))
	}
}

func TestNotFound(t *testing.T) {
	h := &rangeServer{status: http.StatusNotFound}
	srv := httptest.NewServer(h)
	defer srv.Close()

	_, err := Download(context.Background(), newTask(srv, t.TempDir(), 4))
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want 404", err)
	}
}

func TestHeadersCookieAndAuth(t *testing.T) {
	data := testData(64 << 10)
	h := &rangeServer{
		data:           data,
		ranges:         true,
		requireHeaders: map[string]string{"X-Test": "yes"},
		requireCookie:  "session=abc",
		requireUser:    "u",
		requirePass:    "p",
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	task := newTask(srv, t.TempDir(), 4)
	task.Headers = map[string]string{"X-Test": "yes"}
	task.Cookie = "session=abc"
	task.Username = "u"
	task.Password = "p"

	res, err := Download(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
}

func TestContentDispositionName(t *testing.T) {
	data := testData(64 << 10)
	h := &rangeServer{data: data, ranges: true, disposition: `attachment; filename="server-name.bin"`}
	srv := httptest.NewServer(h)
	defer srv.Close()

	dir := t.TempDir()
	res, err := Download(context.Background(), newTask(srv, dir, 4))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(res.Path) != "server-name.bin" {
		t.Fatalf("path = %q", res.Path)
	}
}

func TestResumeFromState(t *testing.T) {
	data := testData(1 << 20)
	h := &rangeServer{data: data, ranges: true}
	srv := httptest.NewServer(h)
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "file.bin")
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(data[:100000], 0); err != nil {
		t.Fatal(err)
	}
	f.Close()
	st := &engine.State{
		URL:  srv.URL + "/file.bin",
		Size: int64(len(data)),
		Segments: []engine.Segment{
			{Start: 0, End: 524287, Done: 100000},
			{Start: 524288, End: 1048575, Done: 0},
		},
	}
	if err := engine.SaveState(dest, st); err != nil {
		t.Fatal(err)
	}

	res, err := Download(context.Background(), newTask(srv, dir, 4))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch after resume")
	}
	found := false
	for _, start := range h.starts() {
		if start == 100000 {
			found = true
		}
	}
	if !found {
		t.Fatalf("resume did not request offset 100000: %v", h.starts())
	}
	if _, err := os.Stat(engine.StatePath(dest)); !os.IsNotExist(err) {
		t.Errorf("state file still present: %v", err)
	}
}

func TestResumeRejectedWhenETagChanges(t *testing.T) {
	data := testData(1 << 20)
	h := &rangeServer{data: data, ranges: true, etag: `"new"`}
	srv := httptest.NewServer(h)
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "file.bin")
	f, _ := os.OpenFile(dest, os.O_CREATE|os.O_RDWR, 0o644)
	_ = f.Truncate(int64(len(data)))
	_, _ = f.WriteAt(data[:100000], 0)
	f.Close()
	st := &engine.State{
		URL:  srv.URL + "/file.bin",
		Size: int64(len(data)),
		ETag: `"old"`,
		Segments: []engine.Segment{
			{Start: 0, End: 524287, Done: 100000},
			{Start: 524288, End: 1048575, Done: 0},
		},
	}
	if err := engine.SaveState(dest, st); err != nil {
		t.Fatal(err)
	}

	res, err := Download(context.Background(), newTask(srv, dir, 4))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch after etag change")
	}
}

func TestCancelSavesState(t *testing.T) {
	data := testData(1 << 20)
	h := &rangeServer{data: data, ranges: true, delay: 5 * time.Millisecond}
	srv := httptest.NewServer(h)
	defer srv.Close()

	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	task := newTask(srv, dir, 8)
	_, err := Download(ctx, task)
	if err == nil {
		t.Fatal("want cancellation error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	dest := filepath.Join(dir, "file.bin")
	st, serr := engine.LoadState(dest)
	if serr != nil {
		t.Fatalf("state not saved: %v", serr)
	}
	if st.Size != int64(len(data)) {
		t.Errorf("state size = %d", st.Size)
	}
	if st.DoneBytes() >= st.Size {
		t.Errorf("state claims complete download")
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("partial file missing: %v", err)
	}
}

func TestParseContentRange(t *testing.T) {
	start, end, total, err := parseContentRange("bytes 100-199/1000")
	if err != nil || start != 100 || end != 199 || total != 1000 {
		t.Fatalf("got %d %d %d %v", start, end, total, err)
	}
	start, end, total, err = parseContentRange("bytes */0")
	if err != nil || start != -1 || end != -1 || total != 0 {
		t.Fatalf("got %d %d %d %v", start, end, total, err)
	}
	_, _, total, err = parseContentRange("bytes 0-0/*")
	if err != nil || total != -1 {
		t.Fatalf("got total %d err %v", total, err)
	}
	if _, _, _, err := parseContentRange("nonsense"); err == nil {
		t.Fatal("want error")
	}
}
