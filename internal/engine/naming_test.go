package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../../evil.txt":      "evil.txt",
		`C:\Users\x\file.txt`: "file.txt",
		`a<b>c:d"e|f?g*h.txt`: "a_b_c_d_e_f_g_h.txt",
		"name. . ":            "name",
		"":                    "download",
		"..":                  "download",
		"CON.txt":             "_CON.txt",
		"nul":                 "_nul",
		"normal-文件.tar.gz":    "normal-文件.tar.gz",
	}
	for in, want := range cases {
		if got := SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeFilenameTruncates(t *testing.T) {
	got := SanitizeFilename(strings.Repeat("a", 400) + ".bin")
	if len([]rune(got)) > 180 {
		t.Errorf("name has %d runes, want <= 180", len([]rune(got)))
	}
	if !strings.HasSuffix(got, ".bin") {
		t.Errorf("extension lost: %q", got)
	}
}

func TestFilenameFromDisposition(t *testing.T) {
	cases := map[string]string{
		`attachment; filename="report.pdf"`:                   "report.pdf",
		`attachment; filename*=UTF-8''%E6%B5%8B%E8%AF%95.bin`: "测试.bin",
		`attachment; filename="../../passwd"`:                 "passwd",
		"inline":                                              "",
		"":                                                    "",
	}
	for in, want := range cases {
		if got := FilenameFromDisposition(in); got != want {
			t.Errorf("FilenameFromDisposition(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFilenameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com/files/a%20b.zip?x=1":  "a b.zip",
		"https://example.com/":                     "",
		"https://example.com/dir/":                 "",
		"https://example.com/%2e%2e%2fevil.bin":    "evil.bin",
		"ftp://user:pass@example.com/pub/file.iso": "file.iso",
	}
	for in, want := range cases {
		if got := FilenameFromURL(in); got != want {
			t.Errorf("FilenameFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveDestAutoName(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveDest(&Task{Dir: dir}, "file.bin")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "file.bin"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveDestCollision(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveDest(&Task{Dir: dir}, "file.bin")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "file (1).bin"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveDestResumesWithState(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(dest, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(StatePath(dest), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveDest(&Task{Dir: dir}, "file.bin")
	if err != nil {
		t.Fatal(err)
	}
	if got != dest {
		t.Fatalf("got %q, want %q", got, dest)
	}
}

func TestResolveDestExplicitExists(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(dest, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveDest(&Task{Dest: dest}, "x.bin"); err == nil {
		t.Fatal("want error for existing explicit output")
	}
	got, err := ResolveDest(&Task{Dest: dest, NoResume: true}, "x.bin")
	if err != nil {
		t.Fatal(err)
	}
	if got != dest {
		t.Fatalf("got %q, want %q", got, dest)
	}
}

func TestResolveDestStaysInDir(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveDest(&Task{Dir: dir}, "../../../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != filepath.Clean(dir) {
		t.Fatalf("dest escaped dir: %q", got)
	}
}
