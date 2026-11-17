package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// reservedNames are Windows device names that cannot be used as file names.
var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true,
	"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
	"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// SanitizeFilename reduces an untrusted name (from a URL or a server header)
// to a safe single path component.
func SanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}

	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"|?*`, r) {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimRight(b.String(), ". ")
	if name == "" || name == "." || name == ".." {
		return "download"
	}

	stem := name
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if reservedNames[strings.ToUpper(stem)] {
		name = "_" + name
	}

	name = truncateRunes(name, 180)
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return "download"
	}
	return name
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	ext := filepath.Ext(s)
	stem := strings.TrimSuffix(s, ext)
	if utf8.RuneCountInString(ext) > 20 {
		ext = ""
		stem = s
	}
	keep := max - utf8.RuneCountInString(ext)
	if keep < 1 {
		keep = 1
	}
	rs := []rune(stem)
	if len(rs) > keep {
		rs = rs[:keep]
	}
	return string(rs) + ext
}

// FilenameFromDisposition extracts a filename from a Content-Disposition
// header, decoding RFC 5987 extended parameters.
func FilenameFromDisposition(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(v)
	if err != nil {
		return ""
	}
	if fn := params["filename"]; fn != "" {
		return SanitizeFilename(fn)
	}
	return ""
}

// FilenameFromURL derives a file name from the last path segment of a URL.
func FilenameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	p := u.Path
	if p == "" || strings.HasSuffix(p, "/") {
		return ""
	}
	name := path.Base(p)
	if name == "" || name == "." || name == "/" {
		return ""
	}
	if dec, err := url.PathUnescape(name); err == nil {
		name = dec
	}
	return SanitizeFilename(name)
}

// StatePath returns the sidecar resume-state path for a destination file.
func StatePath(dest string) string {
	return dest + ".downloadx"
}

// PartsDir returns the temporary parts directory used by HLS and DASH.
func PartsDir(dest string) string {
	return dest + ".parts"
}

// ResolveDest decides the final output path for a download.
//
// It never overwrites an unrelated existing file: a valid resume state keeps
// the same path; otherwise auto-named destinations get a "name (1).ext"
// suffix, and explicitly named destinations return an error.
func ResolveDest(t *Task, suggested string) (string, error) {
	if suggested == "" {
		suggested = "download"
	}
	name := SanitizeFilename(suggested)

	explicit := t.Dest != ""
	dest := t.Dest
	if explicit {
		if info, err := os.Stat(dest); err == nil && info.IsDir() {
			dest = filepath.Join(dest, name)
			explicit = false
		} else if strings.HasSuffix(dest, "/") || strings.HasSuffix(dest, `\`) {
			dest = filepath.Join(dest, name)
			explicit = false
		}
	} else {
		dir := t.Dir
		if dir == "" {
			dir = "."
		}
		dest = filepath.Join(dir, name)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}

	stateExists := fileExists(StatePath(dest))
	if t.NoResume {
		if stateExists {
			_ = os.Remove(StatePath(dest))
		}
		if !explicit && fileExists(dest) {
			return uniquePath(dest), nil
		}
		return dest, nil
	}
	if !fileExists(dest) {
		return dest, nil
	}
	if stateExists {
		return dest, nil
	}
	if explicit {
		return "", fmt.Errorf("output %q already exists; remove it or pass --no-resume to overwrite", dest)
	}
	return uniquePath(dest), nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	if err == nil {
		return true
	}
	return !errors.Is(err, fs.ErrNotExist)
}

func uniquePath(p string) string {
	ext := filepath.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	for i := 1; i < 10000; i++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if !fileExists(cand) {
			return cand
		}
	}
	return fmt.Sprintf("%s (%d)%s", stem, time.Now().UnixNano(), ext)
}
