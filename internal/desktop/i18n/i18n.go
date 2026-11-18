// Package i18n loads interface translations from JSON catalogs.
//
// Built-in catalogs live in locales/*.json. Users add or override languages by
// dropping JSON files into %LOCALAPPDATA%\DownloadX\locales.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neko233-com/DownloadX/internal/inbox"
)

//go:embed locales/*.json
var builtin embed.FS

type catalog struct {
	Name     string            `json:"name"`
	Messages map[string]string `json:"messages"`
}

// Language identifies an available catalog.
type Language struct {
	Code string
	Name string
}

// Bundle holds every loaded catalog and the active language.
type Bundle struct {
	catalogs map[string]*catalog
	current  string
}

// Load loads embedded catalogs and then user catalogs from
// %LOCALAPPDATA%\DownloadX\locales, which override a built-in of the same code.
func Load() *Bundle {
	b := &Bundle{catalogs: map[string]*catalog{}}
	b.loadFS(builtin, "locales")
	if dir, err := UserDir(); err == nil {
		b.loadDir(dir)
	}
	if _, ok := b.catalogs["en"]; !ok && len(b.catalogs) > 0 {
		for code := range b.catalogs {
			b.current = code
			break
		}
	} else {
		b.current = "en"
	}
	return b
}

// UserDir returns the directory for user language catalogs.
func UserDir() (string, error) {
	base, err := inbox.BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "locales"), nil
}

func (b *Bundle) loadFS(fsys interface {
	ReadDir(string) ([]os.DirEntry, error)
	ReadFile(string) ([]byte, error)
}, dir string) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		data, err := fsys.ReadFile(filepath.ToSlash(filepath.Join(dir, entry.Name())))
		if err != nil {
			continue
		}
		b.add(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())), data)
	}
}

func (b *Bundle) loadDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		b.add(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())), data)
	}
}

func (b *Bundle) add(code string, data []byte) {
	var c catalog
	if err := json.Unmarshal(data, &c); err != nil || len(c.Messages) == 0 {
		return
	}
	if c.Name == "" {
		c.Name = code
	}
	b.catalogs[code] = &c
}

// Set switches the active language.
func (b *Bundle) Set(code string) bool {
	if _, ok := b.catalogs[code]; !ok {
		return false
	}
	b.current = code
	return true
}

// Code returns the active language code.
func (b *Bundle) Code() string {
	if b == nil || b.current == "" {
		return "en"
	}
	return b.current
}

// Languages lists the catalogs, English first, then by code.
func (b *Bundle) Languages() []Language {
	if b == nil {
		return nil
	}
	out := make([]Language, 0, len(b.catalogs))
	for code, c := range b.catalogs {
		out = append(out, Language{Code: code, Name: c.Name})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Code == "en" {
			return true
		}
		if out[j].Code == "en" {
			return false
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// T translates key, falling back to English, then to the key itself.
func (b *Bundle) T(key string) string {
	if b == nil {
		return key
	}
	if c, ok := b.catalogs[b.current]; ok {
		if msg, ok := c.Messages[key]; ok {
			return msg
		}
	}
	if c, ok := b.catalogs["en"]; ok {
		if msg, ok := c.Messages[key]; ok {
			return msg
		}
	}
	return key
}

// Tf translates key and applies fmt.Sprintf when args are given.
func (b *Bundle) Tf(key string, args ...any) string {
	msg := b.T(key)
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}
