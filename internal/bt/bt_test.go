package bt

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

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/neko233-com/DownloadX/internal/engine"
)

func buildTorrent(t *testing.T, payload string) (*metainfo.MetaInfo, []byte) {
	t.Helper()
	info := &metainfo.Info{PieceLength: 32 << 10}
	if err := info.BuildFromFilePath(payload); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := &metainfo.MetaInfo{InfoBytes: infoBytes}
	raw, err := bencode.Marshal(mi)
	if err != nil {
		t.Fatal(err)
	}
	return mi, raw
}

func TestAddSourceLocalTorrent(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(payload, bytes.Repeat([]byte("payload"), 16<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	_, raw := buildTorrent(t, payload)
	torrentPath := filepath.Join(dir, "payload.torrent")
	if err := os.WriteFile(torrentPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dir
	cfg.NoDefaultPortForwarding = true
	client, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tor, err := addSource(context.Background(), client, &engine.Task{URL: torrentPath})
	if err != nil {
		t.Fatalf("addSource: %v", err)
	}
	select {
	case <-tor.GotInfo():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for info")
	}
	if got := tor.Info().BestName(); got != "payload.bin" {
		t.Fatalf("name = %q", got)
	}
}

func TestFetchMetaInfoHTTP(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "movie.bin")
	if err := os.WriteFile(payload, bytes.Repeat([]byte("x"), 64<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	_, raw := buildTorrent(t, payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "yes" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	task := &engine.Task{
		URL:     srv.URL + "/movie.torrent",
		Headers: map[string]string{"X-Test": "yes"},
		Timeout: 5 * time.Second,
	}
	mi, err := fetchMetaInfo(context.Background(), task, task.URL)
	if err != nil {
		t.Fatalf("fetchMetaInfo: %v", err)
	}
	if len(mi.InfoBytes) == 0 {
		t.Fatal("empty metainfo")
	}
}

func TestAddSourceRejectsGarbage(t *testing.T) {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.NoDefaultPortForwarding = true
	client, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = addSource(context.Background(), client, &engine.Task{URL: "definitely-not-a-torrent"})
	if err == nil || !strings.Contains(err.Error(), "not a magnet") {
		t.Fatalf("err = %v", err)
	}
}
