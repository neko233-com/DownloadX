package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
)

func TestParseDefaults(t *testing.T) {
	o, err := Parse([]string{"https://example.com/a.zip"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(o.Sources) != 1 || o.Sources[0] != "https://example.com/a.zip" {
		t.Fatalf("sources = %v", o.Sources)
	}
	if o.Connections != engine.DefaultConnections {
		t.Errorf("Connections = %d, want %d", o.Connections, engine.DefaultConnections)
	}
	if o.Type != engine.TypeAuto {
		t.Errorf("Type = %q, want auto", o.Type)
	}
	if o.Retries != 3 {
		t.Errorf("Retries = %d, want 3", o.Retries)
	}
	if o.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", o.Timeout)
	}
	if o.Dir != "." {
		t.Errorf("Dir = %q, want .", o.Dir)
	}
	if o.HLSQuality != "best" {
		t.Errorf("HLSQuality = %q, want best", o.HLSQuality)
	}
}

func TestParseFlagsAfterURL(t *testing.T) {
	o, err := Parse([]string{
		"https://example.com/a.zip",
		"-o", "out.bin",
		"--connections", "4",
		"-q",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if o.Dest != "out.bin" {
		t.Errorf("Dest = %q", o.Dest)
	}
	if o.Connections != 4 {
		t.Errorf("Connections = %d", o.Connections)
	}
	if !o.Quiet {
		t.Error("Quiet = false")
	}
}

func TestParseEqualsForm(t *testing.T) {
	o, err := Parse([]string{"--connections=8", "--type=ftp", "ftp://example.com/f"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if o.Connections != 8 || o.Type != engine.TypeFTP {
		t.Fatalf("connections=%d type=%q", o.Connections, o.Type)
	}
}

func TestParseHeadersAndCredentials(t *testing.T) {
	o, err := Parse([]string{
		"-H", "X-One: 1",
		"--header", "X-Two:  2 ",
		"-u", "user:pass:with:colons",
		"--cookie", "a=b",
		"https://example.com/f",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if o.Headers["X-One"] != "1" || o.Headers["X-Two"] != "2" {
		t.Errorf("headers = %v", o.Headers)
	}
	if o.Username != "user" || o.Password != "pass:with:colons" {
		t.Errorf("user = %q pass = %q", o.Username, o.Password)
	}
	if o.Cookie != "a=b" {
		t.Errorf("cookie = %q", o.Cookie)
	}
}

func TestParseTerminator(t *testing.T) {
	o, err := Parse([]string{"--", "-weird-source"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(o.Sources) != 1 || o.Sources[0] != "-weird-source" {
		t.Fatalf("sources = %v", o.Sources)
	}
}

func TestParseUnknownFlag(t *testing.T) {
	if _, err := Parse([]string{"--nope", "https://example.com/f"}); err == nil {
		t.Fatal("want error for unknown flag")
	}
}

func TestParseConnectionsRange(t *testing.T) {
	for _, v := range []string{"0", "129", "abc"} {
		if _, err := Parse([]string{"-x", v, "https://example.com/f"}); err == nil {
			t.Errorf("want error for -x %s", v)
		}
	}
}

func TestParseInputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "urls.txt")
	body := "https://example.com/a\r\n# comment\n\nhttps://example.com/b\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	o, err := Parse([]string{"-i", path, "https://example.com/c"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{"https://example.com/c", "https://example.com/a", "https://example.com/b"}
	if len(o.Sources) != len(want) {
		t.Fatalf("sources = %v", o.Sources)
	}
	for i := range want {
		if o.Sources[i] != want[i] {
			t.Fatalf("sources = %v, want %v", o.Sources, want)
		}
	}
}

func TestParseRate(t *testing.T) {
	cases := map[string]int64{
		"5M":      5 << 20,
		"800k":    800 << 10,
		"2MB/s":   2 << 20,
		"1048576": 1048576,
		"0":       0,
	}
	for in, want := range cases {
		o, err := Parse([]string{"--limit", in, "https://example.com/f"})
		if err != nil {
			t.Fatalf("limit %q: %v", in, err)
		}
		if o.Limit != want {
			t.Errorf("limit %q = %d, want %d", in, o.Limit, want)
		}
	}
	if _, err := Parse([]string{"--limit", "abc", "https://example.com/f"}); err == nil {
		t.Fatal("want error for bad rate")
	}
}

func TestParseHelpAndVersion(t *testing.T) {
	o, err := Parse([]string{"--help"})
	if err != nil || !o.ShowHelp {
		t.Fatalf("help: %v %+v", err, o)
	}
	o, err = Parse([]string{"-v"})
	if err != nil || !o.ShowVersion {
		t.Fatalf("version: %v %+v", err, o)
	}
}
