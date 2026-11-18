package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.1", "1.0.0", 1},
		{"1.9.9", "1.10.0", -1},
		{"2.0.0", "1.99.99", 1},
		{"1.0.0", "1.0.0-beta", 1},
		{"1.0.0-beta", "1.0.0-alpha", 1},
	}
	for _, tc := range cases {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheckAndDownload(t *testing.T) {
	payload := []byte("fake msi bytes")
	sum := sha256.Sum256(payload)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/releases/latest":
			fmt.Fprintf(w, `{
				"tag_name":"v1.2.3",
				"body":"Release notes",
				"published_at":"2026-11-18T00:00:00Z",
				"html_url":"https://example.test/release",
				"assets":[
					{"name":"DownloadX-windows-amd64.msi","browser_download_url":"%s/msi"},
					{"name":"DownloadX-windows-amd64.msi.sha256","browser_download_url":"%s/sha"}
				]
			}`, tsURL(r), tsURL(r))
		case "/msi":
			_, _ = w.Write(payload)
		case "/sha":
			fmt.Fprintf(w, "%s  DownloadX-windows-amd64.msi\n", hex.EncodeToString(sum[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	oldBase, oldRepo := APIBase, Repo
	APIBase, Repo = ts.URL, "acme/app"
	defer func() { APIBase, Repo = oldBase, oldRepo }()

	rel, err := Check(context.Background(), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil || rel.Version != "1.2.3" || rel.MSIURL == "" || rel.SHAURL == "" {
		t.Fatalf("release = %+v", rel)
	}
	dir := t.TempDir()
	var progress int64
	path, err := DownloadMSI(context.Background(), rel, dir, func(done, total int64) {
		progress = done
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress != int64(len(payload)) {
		t.Errorf("progress = %d, want %d", progress, len(payload))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Errorf("downloaded = %q", got)
	}
	if filepath.Base(path) != "DownloadX-1.2.3-windows-amd64.msi" {
		t.Errorf("name = %q", filepath.Base(path))
	}
}

func TestCheckUpToDate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v1.0.0","assets":[]}`)
	}))
	defer ts.Close()
	oldBase, oldRepo := APIBase, Repo
	APIBase, Repo = ts.URL, "acme/app"
	defer func() { APIBase, Repo = oldBase, oldRepo }()
	rel, err := Check(context.Background(), "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if rel != nil {
		t.Fatalf("release = %+v", rel)
	}
}

func tsURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
