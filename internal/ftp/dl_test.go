package ftp

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
)

func ftpTestData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*17 + i/11)
	}
	return b
}

func newFTPTask(srv *testFTPServer, dir string, conns int) *engine.Task {
	return &engine.Task{
		URL:         srv.URL("/file.bin"),
		Dir:         dir,
		Connections: conns,
		Retries:     1,
		Timeout:     5 * time.Second,
	}
}

func TestDownloadSegmented(t *testing.T) {
	data := ftpTestData(1 << 20)
	srv := newTestFTPServer(t, data)
	task := newFTPTask(srv, t.TempDir(), 4)

	res, err := Download(context.Background(), task)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
	found := false
	for _, off := range srv.restOffsets() {
		if off == 524288 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected REST 524288, got %v", srv.restOffsets())
	}
	if _, err := os.Stat(engine.StatePath(res.Path)); !os.IsNotExist(err) {
		t.Errorf("state file still present: %v", err)
	}
}

func TestDownloadSizeUnsupported(t *testing.T) {
	data := ftpTestData(128 << 10)
	srv := newTestFTPServer(t, data)
	srv.sizeOK = false

	res, err := Download(context.Background(), newFTPTask(srv, t.TempDir(), 8))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch")
	}
}

func TestDownloadRESTUnsupportedFallsBack(t *testing.T) {
	data := ftpTestData(1 << 20)
	srv := newTestFTPServer(t, data)
	srv.restOK = false

	res, err := Download(context.Background(), newFTPTask(srv, t.TempDir(), 4))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch after fallback")
	}
}

func TestDownloadMissing(t *testing.T) {
	srv := newTestFTPServer(t, ftpTestData(1024))
	srv.missing = true

	_, err := Download(context.Background(), newFTPTask(srv, t.TempDir(), 4))
	if err == nil || !strings.Contains(err.Error(), "550") {
		t.Fatalf("err = %v, want 550", err)
	}
}

func TestDownloadEmptyFile(t *testing.T) {
	srv := newTestFTPServer(t, nil)
	res, err := Download(context.Background(), newFTPTask(srv, t.TempDir(), 4))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	info, err := os.Stat(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("size = %d", info.Size())
	}
}

func TestDownloadResume(t *testing.T) {
	data := ftpTestData(1 << 20)
	srv := newTestFTPServer(t, data)

	dir := t.TempDir()
	dest := filepath.Join(dir, "file.bin")
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Truncate(int64(len(data)))
	_, _ = f.WriteAt(data[:100000], 0)
	f.Close()
	st := &engine.State{
		URL:          srv.URL("/file.bin"),
		Size:         int64(len(data)),
		LastModified: "2026-01-01T00:00:00Z",
		Segments: []engine.Segment{
			{Start: 0, End: 524287, Done: 100000},
			{Start: 524288, End: 1048575, Done: 0},
		},
	}
	if err := engine.SaveState(dest, st); err != nil {
		t.Fatal(err)
	}

	res, err := Download(context.Background(), newFTPTask(srv, dir, 4))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, data) {
		t.Fatal("content mismatch after resume")
	}
	found := false
	for _, off := range srv.restOffsets() {
		if off == 100000 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected REST 100000, got %v", srv.restOffsets())
	}
}

func TestDownloadBadCredentials(t *testing.T) {
	srv := newTestFTPServer(t, ftpTestData(1024))
	task := newFTPTask(srv, t.TempDir(), 2)
	task.URL = "ftp://test:wrong@" + srv.ln.Addr().String() + "/file.bin"

	_, err := Download(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "530") {
		t.Fatalf("err = %v, want 530", err)
	}
}
