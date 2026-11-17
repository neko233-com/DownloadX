package dash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/httpx"
	"github.com/neko233-com/DownloadX/internal/segdl"
)

const maxPartSize = 1 << 30

type dashManifest struct {
	URL   string `json:"url"`
	Hash  string `json:"hash"`
	Items int    `json:"items"`
}

type dashDownload struct {
	task   *engine.Task
	client *http.Client
	mpd    *MPD
}

// Download downloads a static DASH manifest.
func Download(ctx context.Context, t *engine.Task) (*engine.Result, error) {
	client, err := httpx.NewClient(t)
	if err != nil {
		return nil, err
	}
	emit(t, "probing", 0, 0, 0)

	data, finalURL, err := fetchMPD(ctx, client, t, t.URL)
	if err != nil {
		return nil, err
	}
	m, err := Parse(finalURL, data)
	if err != nil {
		return nil, err
	}
	video, audio, err := m.PickTracks()
	if err != nil {
		return nil, err
	}

	ext := ".mp4"
	switch {
	case video != nil:
		ext = extFor(video)
	case audio != nil:
		ext = extFor(audio)
	}
	dest, err := engine.ResolveDest(t, suggestName(finalURL)+ext)
	if err != nil {
		return nil, err
	}

	d := &dashDownload{task: t, client: client, mpd: m}
	partsRoot := engine.PartsDir(dest)

	var files []string
	if video != nil {
		target := dest
		if audio != nil {
			target = trimExt(dest) + ".video" + extFor(video)
		}
		if err := d.downloadTrack(ctx, video, target, filepath.Join(partsRoot, "video")); err != nil {
			return nil, err
		}
		files = append(files, target)
	}
	if audio != nil {
		target := dest
		if video != nil {
			target = trimExt(dest) + ".audio" + extFor(audio)
		}
		if err := d.downloadTrack(ctx, audio, target, filepath.Join(partsRoot, "audio")); err != nil {
			return nil, err
		}
		files = append(files, target)
	}

	res := &engine.Result{Files: files}
	switch {
	case video != nil && audio != nil:
		emit(t, "muxing", 0, 0, 0)
		if err := mux(ctx, t.FFmpeg, files[0], files[1], dest); err == nil {
			_ = os.Remove(files[0])
			_ = os.Remove(files[1])
			res.Path = dest
			res.Files = []string{dest}
		} else {
			res.Path = files[0]
		}
	case len(files) == 1:
		res.Path = files[0]
	}

	_ = os.RemoveAll(partsRoot)
	if info, err := os.Stat(res.Path); err == nil {
		res.Bytes = info.Size()
	}
	emit(t, "done", res.Bytes, res.Bytes, 0)
	return res, nil
}

func (d *dashDownload) downloadTrack(ctx context.Context, tr *Track, target, partsDir string) error {
	plan, err := tr.Plan()
	if err != nil {
		return err
	}
	if err := d.ensureParts(partsDir, tr, plan); err != nil {
		return err
	}

	jobs := make([]segdl.Job, 0, len(plan.Segments)+1)
	if plan.Init != nil {
		initRef := *plan.Init
		jobs = append(jobs, segdl.Job{Fetch: func(ctx context.Context) ([]byte, error) {
			return d.fetch(ctx, initRef)
		}})
	}
	for _, seg := range plan.Segments {
		seg := seg
		jobs = append(jobs, segdl.Job{Fetch: func(ctx context.Context) ([]byte, error) {
			return d.fetch(ctx, seg)
		}})
	}

	conns := d.task.ConnectionCount()
	emitTrack(d.task, tr.Kind, 0, len(jobs), conns)
	paths, err := segdl.Run(ctx, jobs, partsDir, conns, func(done, total int, bytes int64) {
		emitTrack(d.task, tr.Kind, done, total, conns)
	})
	if err != nil {
		return err
	}
	return segdl.Concat(paths, target)
}

func (d *dashDownload) ensureParts(dir string, tr *Track, plan *Plan) error {
	m := dashManifest{URL: d.mpd.URL, Hash: planHash(tr, plan), Items: len(plan.Segments)}
	if plan.Init != nil {
		m.Items++
	}
	path := filepath.Join(dir, "manifest.json")
	if data, err := os.ReadFile(path); err == nil {
		var old dashManifest
		if json.Unmarshal(data, &old) == nil && old == m {
			return nil
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func planHash(tr *Track, plan *Plan) string {
	h := sha256.New()
	fmt.Fprintf(h, "track:%s:%s\n", tr.Kind, tr.ID)
	if plan.Init != nil {
		fmt.Fprintf(h, "init:%s\n", plan.Init.URL)
	}
	for _, seg := range plan.Segments {
		fmt.Fprintf(h, "seg:%s\n", seg.URL)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (d *dashDownload) fetch(ctx context.Context, ref SegmentRef) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= d.task.Retries; attempt++ {
		data, retry, err := d.fetchOnce(ctx, ref)
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

func (d *dashDownload) fetchOnce(ctx context.Context, ref SegmentRef) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref.URL, nil)
	if err != nil {
		return nil, false, err
	}
	httpx.ApplyHeaders(req, d.task)
	if ref.Range != nil {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", ref.Range.Start, ref.Range.End))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		retry := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		return nil, retry, fmt.Errorf("dash: http %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPartSize))
	if err != nil {
		return nil, true, err
	}
	if ref.Range != nil {
		if resp.StatusCode == http.StatusOK {
			wantLen := ref.Range.End - ref.Range.Start + 1
			if int64(len(data)) < ref.Range.Start+wantLen {
				return nil, false, fmt.Errorf("dash: range %d-%d exceeds %d-byte response", ref.Range.Start, ref.Range.End, len(data))
			}
			data = data[ref.Range.Start : ref.Range.Start+wantLen]
		}
	}
	return data, false, nil
}

func fetchMPD(ctx context.Context, client *http.Client, t *engine.Task, rawURL string) ([]byte, string, error) {
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
		return nil, "", fmt.Errorf("dash: manifest http %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, "", err
	}
	return data, resp.Request.URL.String(), nil
}

func mux(ctx context.Context, ffmpeg, video, audio, dest string) error {
	path, err := exec.LookPath(ffmpeg)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path,
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", video, "-i", audio, "-c", "copy", dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func suggestName(mpdURL string) string {
	name := engine.FilenameFromURL(mpdURL)
	name = strings.TrimSuffix(name, ".mpd")
	if name == "" || name == "download" {
		name = "video"
	}
	return name
}

func trimExt(path string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext)
}

func extFor(tr *Track) string {
	mime := strings.ToLower(tr.MimeType)
	switch {
	case strings.Contains(mime, "webm") && tr.Kind == "audio":
		return ".weba"
	case strings.Contains(mime, "webm"):
		return ".webm"
	case tr.Kind == "audio":
		return ".m4a"
	default:
		return ".mp4"
	}
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

func emitTrack(t *engine.Task, kind string, done, total, conns int) {
	if t.Progress == nil {
		return
	}
	t.Progress(engine.Progress{
		Phase:       "downloading",
		ItemsDone:   done,
		ItemsTotal:  total,
		Connections: conns,
		Message:     kind,
	})
}
