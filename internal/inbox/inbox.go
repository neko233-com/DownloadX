// Package inbox carries download requests from the browser extensions to the
// desktop app through small JSON files.
package inbox

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Request is one browser hand-off.
type Request struct {
	URL      string `json:"url"`
	Filename string `json:"filename,omitempty"`
	Referrer string `json:"referrer,omitempty"`
}

// Item is a pending request and the file that holds it.
type Item struct {
	Path    string
	Request Request
}

// BaseDir is the app's per-user data directory.
func BaseDir() (string, error) {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return filepath.Join(dir, "DownloadX"), nil
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "DownloadX"), nil
	}
	return "", errors.New("cannot find a per-user data directory")
}

// Dir is the inbox directory.
func Dir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "inbox"), nil
}

// Enqueue writes a request atomically for the desktop app to pick up.
func Enqueue(req Request) error {
	if strings.TrimSpace(req.URL) == "" {
		return errors.New("inbox: empty URL")
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	name := fmt.Sprintf("request-%d-%s.json", time.Now().UnixNano(), hex.EncodeToString(suffix))
	tmp := filepath.Join(dir, ".tmp-"+name)
	final := filepath.Join(dir, name)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Pending returns the queued requests in arrival order.
func Pending() ([]Item, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	items := make([]Item, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req Request
		if err := json.Unmarshal(data, &req); err != nil || strings.TrimSpace(req.URL) == "" {
			_ = os.Remove(path)
			continue
		}
		items = append(items, Item{Path: path, Request: req})
	}
	return items, nil
}

// Remove deletes a processed request.
func Remove(path string) {
	_ = os.Remove(path)
}

// Logf appends a line to the host log, never failing the caller.
func Logf(format string, args ...any) {
	base, err := BaseDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(base, "host.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}
