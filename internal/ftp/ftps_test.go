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

func TestDownloadExplicitFTPS(t *testing.T) {
	data := bytes.Repeat([]byte("ftps-payload-"), 32<<10)
	srv := newTestFTPServer(t, data)
	srv.enableTLS(t)

	dir := t.TempDir()
	dest := filepath.Join(dir, "payload.bin")
	task := &engine.Task{
		URL:         strings.Replace(srv.URL("/payload.bin"), "ftp://", "ftps://", 1),
		Dest:        dest,
		Connections: 4,
		Timeout:     10 * time.Second,
		Insecure:    true,
	}
	res, err := Download(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Path != dest {
		t.Fatalf("result = %+v", res)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(data))
	}
	auths, dataConns := srv.tlsCounts()
	if auths == 0 || dataConns == 0 {
		t.Fatalf("TLS not used: control=%d data=%d", auths, dataConns)
	}
}
