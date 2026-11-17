// Package bt downloads BitTorrent magnets and .torrent files.
package bt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/time/rate"

	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/httpx"
)

const maxTorrentFileSize = 32 << 20

// Download downloads a magnet link, a .torrent file path, or an HTTP(S) URL
// of a .torrent file into the task's destination directory.
func Download(ctx context.Context, t *engine.Task) (*engine.Result, error) {
	dataDir := t.Dest
	if dataDir == "" {
		dataDir = t.Dir
	}
	if dataDir == "" {
		dataDir = "."
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("bittorrent: create data dir: %w", err)
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir
	cfg.ListenPort = t.BTPort
	cfg.Seed = t.Seed
	cfg.NoDefaultPortForwarding = true
	if t.Limit > 0 {
		burst := int(t.Limit)
		if burst > 256*1024 {
			burst = 256 * 1024
		}
		if burst < 16*1024 {
			burst = 16 * 1024
		}
		cfg.DownloadRateLimiter = rate.NewLimiter(rate.Limit(t.Limit), burst)
		cfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(t.Limit), burst)
	}

	client, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("bittorrent: start client: %w", err)
	}
	defer client.Close()

	emit(t, "probing", 0, 0, 0, "fetching metadata")
	tor, err := addSource(ctx, client, t)
	if err != nil {
		return nil, err
	}

	infoTimeout := 2 * time.Minute
	if t.Timeout > infoTimeout {
		infoTimeout = t.Timeout
	}
	select {
	case <-tor.GotInfo():
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(infoTimeout):
		return nil, errors.New("bittorrent: timed out waiting for metadata")
	}
	info := tor.Info()
	if info == nil {
		return nil, errors.New("bittorrent: torrent has no metadata")
	}
	tor.DownloadAll()

	total := tor.Length()
	path := filepath.Join(dataDir, info.BestName())

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		done := tor.BytesCompleted()
		stats := tor.Stats()
		emit(t, "downloading", done, total, stats.ActivePeers, "peers")
		if tor.Complete().Bool() {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}

	res := &engine.Result{Path: path, Bytes: total, Files: []string{path}}
	if t.Seed {
		emit(t, "seeding", total, total, tor.Stats().ActivePeers, "seeding")
		<-ctx.Done()
		return res, nil
	}
	emit(t, "done", total, total, 0, "")
	return res, nil
}

func addSource(ctx context.Context, client *torrent.Client, t *engine.Task) (*torrent.Torrent, error) {
	src := strings.TrimSpace(t.URL)
	switch {
	case strings.HasPrefix(src, "magnet:"):
		tor, err := client.AddMagnet(src)
		if err != nil {
			return nil, fmt.Errorf("bittorrent: invalid magnet: %w", err)
		}
		return tor, nil
	case strings.HasPrefix(src, "http://"), strings.HasPrefix(src, "https://"):
		mi, err := fetchMetaInfo(ctx, t, src)
		if err != nil {
			return nil, err
		}
		tor, err := client.AddTorrent(mi)
		if err != nil {
			return nil, fmt.Errorf("bittorrent: add torrent: %w", err)
		}
		return tor, nil
	default:
		if info, err := os.Stat(src); err == nil && !info.IsDir() {
			tor, err := client.AddTorrentFromFile(src)
			if err != nil {
				return nil, fmt.Errorf("bittorrent: load %s: %w", src, err)
			}
			return tor, nil
		}
		return nil, fmt.Errorf("bittorrent: source is not a magnet link or .torrent file: %s", src)
	}
}

func fetchMetaInfo(ctx context.Context, t *engine.Task, rawURL string) (*metainfo.MetaInfo, error) {
	client, err := httpx.NewClient(t)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	httpx.ApplyHeaders(req, t)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("bittorrent: fetch .torrent: http %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTorrentFileSize))
	if err != nil {
		return nil, err
	}
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("bittorrent: parse .torrent: %w", err)
	}
	return mi, nil
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
