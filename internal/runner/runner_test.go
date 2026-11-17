package runner

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
)

func TestDetectSchemes(t *testing.T) {
	cases := map[string]engine.Type{
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567": engine.TypeBT,
		"ftp://example.com/file.bin":                                   engine.TypeFTP,
		"ftps://example.com/file.bin":                                  engine.TypeFTP,
		"https://example.com/video.m3u8":                               engine.TypeHLS,
		"https://example.com/manifest.mpd":                             engine.TypeDASH,
	}
	for src, want := range cases {
		got, err := detect(context.Background(), &engine.Task{URL: src, Timeout: time.Second})
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", src, got, want)
		}
	}
}

func TestDetectLocalTorrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.torrent")
	if err := os.WriteFile(path, []byte("d4:name4:teste"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := detect(context.Background(), &engine.Task{URL: path})
	if err != nil || got != engine.TypeBT {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestDetectLocalOtherRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := detect(context.Background(), &engine.Task{URL: path}); err == nil {
		t.Fatal("want error for local non-torrent")
	}
}

func TestDetectContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hls":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl; charset=utf-8")
		case "/dash":
			w.Header().Set("Content-Type", "application/dash+xml")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	cases := map[string]engine.Type{
		srv.URL + "/hls":  engine.TypeHLS,
		srv.URL + "/dash": engine.TypeDASH,
		srv.URL + "/raw":  engine.TypeHTTP,
	}
	for src, want := range cases {
		got, err := detect(context.Background(), &engine.Task{URL: src, Timeout: 2 * time.Second})
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", src, got, want)
		}
	}
}

func TestRunHTTP(t *testing.T) {
	payload := bytes.Repeat([]byte("hello"), 10000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "50000")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	res, err := Run(context.Background(), &engine.Task{
		URL:         srv.URL + "/file.bin",
		Dir:         dir,
		Connections: 4,
		Retries:     0,
		Timeout:     2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("content mismatch")
	}
}

func TestRunUnsupported(t *testing.T) {
	_, err := Run(context.Background(), &engine.Task{URL: "gopher://example.com/x"})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("err = %v", err)
	}
}
