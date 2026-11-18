package i18n

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinCatalogs(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	b := Load()
	if !b.Set("en") {
		t.Fatal("missing en catalog")
	}
	if got := b.T("nav.downloads"); got != "Downloads" {
		t.Fatalf("en nav.downloads = %q", got)
	}
	if !b.Set("zh-Hans") {
		t.Fatal("missing zh-Hans catalog")
	}
	if got := b.T("nav.downloads"); got != "下载" {
		t.Fatalf("zh nav.downloads = %q", got)
	}
	if b.T("missing.key") != "missing.key" {
		t.Fatal("missing key should fall back to itself")
	}
	if len(b.Languages()) < 2 {
		t.Fatalf("languages = %+v", b.Languages())
	}
}

func TestMatchPreferredLanguages(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	b := Load()
	cases := map[string]string{
		"zh-CN": "zh-Hans",
		"zh-SG": "zh-Hans",
		"zh-TW": "zh-Hant",
		"zh-HK": "zh-Hant",
		"en-GB": "en",
	}
	for tag, want := range cases {
		if got := b.Match([]string{tag}); got != want {
			t.Errorf("Match(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestExternalCatalog(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir, err := UserDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"name":"Français","messages":{"nav.downloads":"Téléchargements"}}`)
	if err := os.WriteFile(filepath.Join(dir, "fr.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	b := Load()
	if !b.Set("fr") {
		t.Fatal("external fr catalog not loaded")
	}
	if got := b.T("nav.downloads"); got != "Téléchargements" {
		t.Fatalf("nav.downloads = %q", got)
	}
	if got := b.T("nav.settings"); got != "Settings" {
		t.Fatalf("fallback = %q", got)
	}
}
