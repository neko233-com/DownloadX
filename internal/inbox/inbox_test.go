package inbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnqueuePendingRemove(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())

	if err := Enqueue(Request{URL: "https://example.com/a.zip", Filename: "a.zip"}); err != nil {
		t.Fatal(err)
	}
	if err := Enqueue(Request{URL: "https://example.com/b.zip"}); err != nil {
		t.Fatal(err)
	}
	items, err := Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	if items[0].Request.URL != "https://example.com/a.zip" {
		t.Fatalf("first = %+v", items[0])
	}
	for _, item := range items {
		Remove(item.Path)
	}
	items, err = Pending()
	if err != nil || len(items) != 0 {
		t.Fatalf("after remove: %d %v", len(items), err)
	}
}

func TestPendingDropsInvalidFiles(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "request-bad.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %d", len(items))
	}
	if _, err := os.Stat(filepath.Join(dir, "request-bad.json")); !os.IsNotExist(err) {
		t.Fatal("invalid request was not removed")
	}
}
