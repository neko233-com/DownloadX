package progress

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/neko233-com/DownloadX/internal/engine"
)

func TestJSONProgress(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, ModeJSON)
	r.SetLabel("https://example.com/file.bin")
	r.Update(engine.Progress{Phase: "downloading", Done: 50, Total: 100, Connections: 16})
	r.Done(&engine.Result{Path: "file.bin", Bytes: 100}, nil)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d: %q", len(lines), buf.String())
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["event"] != "progress" || first["done"].(float64) != 50 || first["connections"].(float64) != 16 {
		t.Fatalf("first = %v", first)
	}
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &last); err != nil {
		t.Fatal(err)
	}
	if last["event"] != "done" || last["path"] != "file.bin" {
		t.Fatalf("last = %v", last)
	}
}

func TestJSONError(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, ModeJSON)
	r.Done(nil, errTest("boom"))
	var v map[string]any
	if err := json.Unmarshal(buf.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v["event"] != "error" || !strings.Contains(v["error"].(string), "boom") {
		t.Fatalf("v = %v", v)
	}
}

func TestPlainProgress(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, ModePlain)
	r.Update(engine.Progress{Phase: "downloading", Done: 2048, Total: 4096})
	out := buf.String()
	if !strings.Contains(out, "50.0%") || !strings.Contains(out, "2.0 KB") {
		t.Fatalf("out = %q", out)
	}
}

func TestTTYProgress(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, ModeTTY)
	r.Update(engine.Progress{Phase: "downloading", Done: 25, Total: 100, Connections: 4})
	out := buf.String()
	if !strings.HasPrefix(out, "\r\x1b[K") || !strings.Contains(out, "25.0%") || !strings.Contains(out, "×4") {
		t.Fatalf("out = %q", out)
	}
	buf.Reset()
	r.Done(&engine.Result{Path: "file.bin"}, nil)
	if buf.String() != "\r\x1b[K" {
		t.Fatalf("done out = %q", buf.String())
	}
}

func TestItemsProgress(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, ModeTTY)
	r.Update(engine.Progress{Phase: "downloading", ItemsDone: 3, ItemsTotal: 12, Message: "segments"})
	out := buf.String()
	if !strings.Contains(out, "3/12") || !strings.Contains(out, "segments") {
		t.Fatalf("out = %q", out)
	}
}

func TestQuiet(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, ModeQuiet)
	r.Update(engine.Progress{Done: 1, Total: 2})
	r.Done(&engine.Result{Path: "x"}, nil)
	if buf.Len() != 0 {
		t.Fatalf("quiet wrote %q", buf.String())
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
