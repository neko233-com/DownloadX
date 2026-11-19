package bt

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/neko233-com/DownloadX/internal/engine"
)

// TestDownloadFromLocalSeeder exercises a real peer transfer: a seeder
// announces to a tiny HTTP tracker, then Download fetches the payload from it.
func TestDownloadFromLocalSeeder(t *testing.T) {
	payload := bytes.Repeat([]byte("bittorrent-local-transfer-"), 4096)
	seedDir := t.TempDir()
	payloadPath := filepath.Join(seedDir, "payload.bin")
	if err := os.WriteFile(payloadPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	var (
		mu         sync.Mutex
		seederPort int
	)
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		left, _ := strconv.ParseInt(q.Get("left"), 10, 64)
		mu.Lock()
		sp := seederPort
		mu.Unlock()
		peers := []byte{}
		if sp > 0 && left > 0 {
			peers = []byte{127, 0, 0, 1, byte(sp >> 8), byte(sp)}
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintf(w, "d8:intervali60e5:peers%d:%se", len(peers), peers)
	}))
	defer tracker.Close()

	info := &metainfo.Info{PieceLength: 32 << 10}
	if err := info.BuildFromFilePath(payloadPath); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := &metainfo.MetaInfo{
		InfoBytes: infoBytes,
		Announce:  tracker.URL + "/announce",
	}
	torrentPath := filepath.Join(seedDir, "payload.torrent")
	raw, err := bencode.Marshal(mi)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(torrentPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = seedDir
	cfg.Seed = true
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	seeder, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer seeder.Close()
	if port := seeder.LocalPort(); port > 0 {
		mu.Lock()
		seederPort = port
		mu.Unlock()
	} else {
		t.Fatal("seeder has no listening port")
	}
	tor, err := seeder.AddTorrent(mi)
	if err != nil {
		t.Fatal(err)
	}
	<-tor.GotInfo()
	if err := tor.VerifyData(); err != nil {
		t.Fatal(err)
	}
	if !tor.Complete().Bool() {
		t.Fatal("seeder did not verify its payload")
	}

	downloadDir := t.TempDir()
	res, err := Download(context.Background(), &engine.Task{
		URL:     torrentPath,
		Dir:     downloadDir,
		BTPort:  52100,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Path == "" {
		t.Fatalf("result = %+v", res)
	}
	got, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(payload))
	}
}
