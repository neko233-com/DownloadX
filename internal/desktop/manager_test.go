package desktop

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/inbox"
)

func waitStatus(t *testing.T, m *Manager, id string, want Status) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range m.Snapshot() {
			if j.ID == id && j.Status == want {
				return j
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, j := range m.Snapshot() {
		if j.ID == id {
			t.Fatalf("job stuck in %s: %+v", j.Status, j)
		}
	}
	t.Fatalf("job %s not found", id)
	return Job{}
}

func TestManagerDownloadsHTTP(t *testing.T) {
	payload := bytes.Repeat([]byte("downloadx"), 8192)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "73728")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	m := NewManager(2)
	defer m.Stop()

	job, err := m.Add(Request{URL: srv.URL + "/file.bin", Dir: t.TempDir(), Connections: 4, Type: engine.TypeAuto})
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, m, job.ID, StatusDone)
	if done.Bytes != int64(len(payload)) {
		t.Fatalf("bytes = %d, want %d", done.Bytes, len(payload))
	}
	got, err := os.ReadFile(done.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("content mismatch")
	}
	if done.Started.IsZero() || done.Finished.IsZero() {
		t.Fatal("timestamps not set")
	}
}

func TestManagerCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		for i := 0; i < 64; i++ {
			if _, err := w.Write(bytes.Repeat([]byte("x"), 16*1024)); err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer srv.Close()

	m := NewManager(1)
	defer m.Stop()
	job, err := m.Add(Request{URL: srv.URL + "/slow.bin", Dir: t.TempDir(), Connections: 4})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, job.ID, StatusRunning)
	m.Cancel(job.ID)
	waitStatus(t, m, job.ID, StatusCanceled)
}

func TestManagerRejectsEmptyURL(t *testing.T) {
	m := NewManager(1)
	defer m.Stop()
	if _, err := m.Add(Request{URL: "   "}); err == nil {
		t.Fatal("want error")
	}
}

func TestManagerClearFinished(t *testing.T) {
	m := NewManager(1)
	defer m.Stop()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	job, err := m.Add(Request{URL: srv.URL + "/ok.bin", Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, job.ID, StatusDone)
	if !m.HasFinished() {
		t.Fatal("HasFinished = false")
	}
	m.ClearFinished()
	if len(m.Snapshot()) != 0 {
		t.Fatalf("jobs left: %d", len(m.Snapshot()))
	}

	if err := m.Retry("missing"); err == nil {
		t.Fatal("want retry error")
	}
}

func TestManagerBrowserInbox(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	payload := bytes.Repeat([]byte("browser"), 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "28672")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	if err := inbox.Enqueue(inbox.Request{URL: srv.URL + "/from-browser.bin"}); err != nil {
		t.Fatal(err)
	}

	m := NewManager(1)
	defer m.Stop()
	dir := t.TempDir()
	m.StartInbox(dir)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range m.Snapshot() {
			if j.Status == StatusDone {
				got, err := os.ReadFile(j.Path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, payload) {
					t.Fatal("content mismatch")
				}
				if _, err := inbox.Pending(); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("browser download never finished")
}
