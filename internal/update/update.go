// Package update checks GitHub Releases for a newer DownloadX MSI, verifies
// its published SHA-256 and hands installation to Windows Installer.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Repo and APIBase are variables so tests can point at a local server.
var (
	Repo    = "neko233-com/DownloadX"
	APIBase = "https://api.github.com"
)

// Release is a newer published version.
type Release struct {
	Version   string
	Notes     string
	Published time.Time
	MSIURL    string
	SHAURL    string
	HTMLURL   string
}

type apiRelease struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
	Draft      bool `json:"draft"`
	Prerelease bool `json:"prerelease"`
}

// Check returns the latest stable release when it is newer than current, or
// nil when the app is up to date.
func Check(ctx context.Context, current string) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(APIBase, "/"), Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "DownloadX/"+strings.TrimPrefix(current, "v"))
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("no published release found")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("update check: http %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var rel apiRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("update check: %w", err)
	}
	if rel.Draft || rel.Prerelease {
		return nil, nil
	}
	latest := normalizeVersion(rel.TagName)
	if latest == "" {
		return nil, errors.New("update check: release has no version")
	}
	if Compare(latest, current) <= 0 {
		return nil, nil
	}
	out := &Release{
		Version:   latest,
		Notes:     rel.Body,
		Published: rel.PublishedAt,
		HTMLURL:   rel.HTMLURL,
	}
	for _, asset := range rel.Assets {
		switch asset.Name {
		case "DownloadX-windows-amd64.msi":
			out.MSIURL = asset.BrowserDownloadURL
		case "DownloadX-windows-amd64.msi.sha256":
			out.SHAURL = asset.BrowserDownloadURL
		}
	}
	if out.MSIURL == "" {
		return nil, errors.New("update check: release has no Windows MSI")
	}
	if out.SHAURL == "" {
		return nil, errors.New("update check: release has no MSI checksum")
	}
	return out, nil
}

// DownloadMSI downloads and verifies the release into dir.
func DownloadMSI(ctx context.Context, rel *Release, dir string, progress func(done, total int64)) (string, error) {
	if rel == nil || rel.MSIURL == "" {
		return "", errors.New("update: no MSI to download")
	}
	want, err := fetchChecksum(ctx, rel.SHAURL)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := "DownloadX-" + rel.Version + "-windows-amd64.msi"
	dest := filepath.Join(dir, name)
	tmp := dest + ".part"
	_ = os.Remove(tmp)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.MSIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "DownloadX")
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update download: http %s", resp.Status)
	}
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := writer.Write(buf[:n]); err != nil {
				f.Close()
				_ = os.Remove(tmp)
				return "", err
			}
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			_ = os.Remove(tmp)
			return "", readErr
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(got, want) {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("update download: checksum mismatch (got %s, want %s)", got, want)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dest, nil
}

func fetchChecksum(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "DownloadX")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update checksum: http %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", errors.New("update checksum: malformed checksum file")
	}
	return fields[0], nil
}

// Apply starts a detached helper that waits for this process to exit, runs
// Windows Installer passively, and starts the app again. The caller should
// quit after Apply returns.
func Apply(msiPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "DownloadX")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	script := filepath.Join(dir, "apply-update.cmd")
	quotedMSI := strings.ReplaceAll(msiPath, `"`, `""`)
	quotedExe := strings.ReplaceAll(exe, `"`, `""`)
	body := "@echo off\r\n" +
		"ping 127.0.0.1 -n 3 >nul\r\n" +
		"msiexec /i \"" + quotedMSI + "\" /passive /norestart\r\n" +
		"start \"\" \"" + quotedExe + "\"\r\n" +
		"del \"%~f0\" >nul 2>&1\r\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		return err
	}
	cmd := exec.Command("cmd.exe", "/c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

// Compare returns -1, 0 or 1 as a is older than, equal to or newer than b.
// It compares dotted numeric releases, with prereleases older than the final
// release of the same version.
func Compare(a, b string) int {
	av, apre := parseVersion(a)
	bv, bpre := parseVersion(b)
	for i := 0; i < len(av) || i < len(bv); i++ {
		var x, y int
		if i < len(av) {
			x = av[i]
		}
		if i < len(bv) {
			y = bv[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case apre == "" && bpre != "":
		return 1
	case apre != "" && bpre == "":
		return -1
	case apre < bpre:
		return -1
	case apre > bpre:
		return 1
	}
	return 0
}

func parseVersion(v string) ([]int, string) {
	v = normalizeVersion(v)
	pre := ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, _ := strconv.Atoi(strings.TrimSpace(part))
		out = append(out, n)
	}
	return out, pre
}
