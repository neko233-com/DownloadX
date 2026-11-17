package hls

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/httpx"
	"github.com/neko233-com/DownloadX/internal/segdl"
)

const maxPartSize = 1 << 30

type hlsDownload struct {
	task     *engine.Task
	client   *http.Client
	mediaURL string
	pl       *Playlist
	initSeg  *Map
	dest     string
	partsDir string
	keys     map[string][]byte
	keyMu    sync.Mutex
}

type manifest struct {
	URL   string `json:"url"`
	Hash  string `json:"hash"`
	Items int    `json:"items"`
}

// Download downloads an HLS VOD playlist.
func Download(ctx context.Context, t *engine.Task) (*engine.Result, error) {
	client, err := httpx.NewClient(t)
	if err != nil {
		return nil, err
	}
	emit(t, "probing", 0, 0, 0)

	data, finalURL, err := fetchPlaylist(ctx, client, t, t.URL)
	if err != nil {
		return nil, err
	}
	pl, err := Parse(finalURL, data)
	if err != nil {
		return nil, err
	}
	mediaURL := finalURL
	if pl.IsMaster() {
		variant, err := pl.PickVariant(t.HLSQuality)
		if err != nil {
			return nil, err
		}
		data, finalURL, err = fetchPlaylist(ctx, client, t, variant.URI)
		if err != nil {
			return nil, err
		}
		mediaURL = finalURL
		pl, err = Parse(mediaURL, data)
		if err != nil {
			return nil, err
		}
		if pl.IsMaster() {
			return nil, errors.New("hls: master playlist points to another master playlist")
		}
	}

	if !pl.EndList {
		return nil, errors.New("hls: live playlists are not supported (missing EXT-X-ENDLIST)")
	}
	if len(pl.Segments) == 0 {
		return nil, errors.New("hls: playlist has no media segments")
	}
	for _, seg := range pl.Segments {
		if seg.Key != nil && !strings.EqualFold(seg.Key.Method, "AES-128") {
			return nil, fmt.Errorf("hls: unsupported encryption method %q (only AES-128 is supported)", seg.Key.Method)
		}
	}

	initSeg, err := pickInit(pl)
	if err != nil {
		return nil, err
	}
	ext := ".ts"
	if initSeg != nil {
		ext = ".mp4"
	}
	dest, err := engine.ResolveDest(t, suggestName(mediaURL, ext))
	if err != nil {
		return nil, err
	}

	d := &hlsDownload{
		task:     t,
		client:   client,
		mediaURL: mediaURL,
		pl:       pl,
		initSeg:  initSeg,
		dest:     dest,
		partsDir: engine.PartsDir(dest),
		keys:     map[string][]byte{},
	}
	return d.run(ctx)
}

func (d *hlsDownload) run(ctx context.Context) (*engine.Result, error) {
	if err := d.ensureParts(); err != nil {
		return nil, err
	}
	conns := d.task.ConnectionCount()
	jobs := d.jobs()
	emitProgress(d.task, 0, len(jobs), conns)

	paths, err := segdl.Run(ctx, jobs, d.partsDir, conns, func(done, total int, bytes int64) {
		emitProgress(d.task, done, total, conns)
	})
	if err != nil {
		return nil, err
	}
	if err := segdl.Concat(paths, d.dest); err != nil {
		return nil, err
	}
	_ = os.RemoveAll(d.partsDir)

	var size int64
	if info, err := os.Stat(d.dest); err == nil {
		size = info.Size()
	}
	emit(d.task, "done", size, size, conns)
	return &engine.Result{Path: d.dest, Bytes: size}, nil
}

func (d *hlsDownload) jobs() []segdl.Job {
	jobs := make([]segdl.Job, 0, len(d.pl.Segments)+1)
	if d.initSeg != nil {
		initSeg := d.initSeg
		jobs = append(jobs, segdl.Job{Fetch: func(ctx context.Context) ([]byte, error) {
			return d.fetchRaw(ctx, initSeg.URI, initSeg.ByteRange)
		}})
	}
	for _, seg := range d.pl.Segments {
		seg := seg
		jobs = append(jobs, segdl.Job{Fetch: func(ctx context.Context) ([]byte, error) {
			data, err := d.fetchRaw(ctx, seg.URI, seg.ByteRange)
			if err != nil {
				return nil, err
			}
			if seg.Key == nil {
				return data, nil
			}
			key, err := d.keyBytes(ctx, seg.Key.URI)
			if err != nil {
				return nil, err
			}
			iv, err := parseIV(seg.Key.IV, seg.Sequence)
			if err != nil {
				return nil, err
			}
			plain, err := decryptAES128(key, iv, data)
			if err != nil {
				return nil, fmt.Errorf("hls: decrypt %s: %w", seg.URI, err)
			}
			return plain, nil
		}})
	}
	return jobs
}

func pickInit(pl *Playlist) (*Map, error) {
	var initSeg *Map
	for _, seg := range pl.Segments {
		if seg.Map == nil {
			continue
		}
		if initSeg == nil {
			initSeg = seg.Map
			continue
		}
		if initSeg.URI != seg.Map.URI {
			return nil, errors.New("hls: multiple distinct EXT-X-MAP initialization segments are not supported")
		}
	}
	return initSeg, nil
}

func (d *hlsDownload) ensureParts() error {
	m := manifest{
		URL:   d.mediaURL,
		Hash:  d.planHash(),
		Items: len(d.pl.Segments),
	}
	if d.initSeg != nil {
		m.Items++
	}
	path := filepath.Join(d.partsDir, "manifest.json")
	if data, err := os.ReadFile(path); err == nil {
		var old manifest
		if json.Unmarshal(data, &old) == nil && old.URL == m.URL && old.Hash == m.Hash && old.Items == m.Items {
			return nil
		}
	}
	if err := os.RemoveAll(d.partsDir); err != nil {
		return err
	}
	if err := os.MkdirAll(d.partsDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (d *hlsDownload) planHash() string {
	h := sha256.New()
	if d.initSeg != nil {
		fmt.Fprintf(h, "map:%s\n", d.initSeg.URI)
	}
	for _, seg := range d.pl.Segments {
		fmt.Fprintf(h, "seg:%s:%d\n", seg.URI, seg.Sequence)
		if seg.Key != nil {
			fmt.Fprintf(h, "key:%s\n", seg.Key.URI)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (d *hlsDownload) keyBytes(ctx context.Context, uri string) ([]byte, error) {
	d.keyMu.Lock()
	if b, ok := d.keys[uri]; ok {
		d.keyMu.Unlock()
		return b, nil
	}
	d.keyMu.Unlock()

	b, err := d.fetchRaw(ctx, uri, nil)
	if err != nil {
		return nil, err
	}
	if len(b) != 16 {
		return nil, fmt.Errorf("hls: AES-128 key is %d bytes, want 16", len(b))
	}
	d.keyMu.Lock()
	d.keys[uri] = b
	d.keyMu.Unlock()
	return b, nil
}

func (d *hlsDownload) fetchRaw(ctx context.Context, rawURL string, br *ByteRange) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= d.task.Retries; attempt++ {
		data, retry, err := d.fetchOnce(ctx, rawURL, br)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !retry || attempt >= d.task.Retries {
			break
		}
		if err := sleepBackoff(ctx, attempt); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (d *hlsDownload) fetchOnce(ctx context.Context, rawURL string, br *ByteRange) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	httpx.ApplyHeaders(req, d.task)
	if br != nil {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", br.Offset, br.Offset+br.Length-1))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		retry := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		return nil, retry, fmt.Errorf("hls: http %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPartSize))
	if err != nil {
		return nil, true, err
	}
	if br != nil {
		if resp.StatusCode == http.StatusOK {
			if int64(len(data)) < br.Offset+br.Length {
				return nil, false, fmt.Errorf("hls: byte range %d+%d exceeds %d-byte response", br.Offset, br.Length, len(data))
			}
			data = data[br.Offset : br.Offset+br.Length]
		} else if int64(len(data)) != br.Length {
			return nil, false, fmt.Errorf("hls: byte range returned %d bytes, want %d", len(data), br.Length)
		}
	}
	return data, false, nil
}

func fetchPlaylist(ctx context.Context, client *http.Client, t *engine.Task, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	httpx.ApplyHeaders(req, t)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, "", fmt.Errorf("hls: playlist http %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, "", err
	}
	return data, resp.Request.URL.String(), nil
}

func suggestName(playlistURL, ext string) string {
	name := engine.FilenameFromURL(playlistURL)
	name = strings.TrimSuffix(name, ".m3u8")
	name = strings.TrimSuffix(name, ".m3u")
	if name == "" || name == "download" {
		name = "video"
	}
	return name + ext
}

func parseIV(s string, seq int64) ([]byte, error) {
	if s == "" {
		iv := make([]byte, 16)
		binary.BigEndian.PutUint64(iv[8:], uint64(seq))
		return iv, nil
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("hls: bad IV: %w", err)
	}
	if len(b) != 16 {
		return nil, fmt.Errorf("hls: IV is %d bytes, want 16", len(b))
	}
	return b, nil
}

func decryptAES128(key, iv, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("hls: empty encrypted segment")
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("hls: encrypted length %d is not a multiple of 16", len(data))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)

	pad := int(out[len(out)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(out) {
		return nil, errors.New("hls: invalid PKCS#7 padding")
	}
	for _, b := range out[len(out)-pad:] {
		if int(b) != pad {
			return nil, errors.New("hls: invalid PKCS#7 padding")
		}
	}
	return out[:len(out)-pad], nil
}

func sleepBackoff(ctx context.Context, attempt int) error {
	wait := time.Duration(1<<uint(min(attempt, 4))) * 500 * time.Millisecond
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func emit(t *engine.Task, phase string, done, total int64, conns int) {
	if t.Progress == nil {
		return
	}
	t.Progress(engine.Progress{
		Phase:       phase,
		Done:        done,
		Total:       total,
		Connections: conns,
	})
}

func emitProgress(t *engine.Task, done, total, conns int) {
	if t.Progress == nil {
		return
	}
	t.Progress(engine.Progress{
		Phase:       "downloading",
		ItemsDone:   done,
		ItemsTotal:  total,
		Connections: conns,
		Message:     "segments",
	})
}
