package segdl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestRunAndConcat(t *testing.T) {
	dir := t.TempDir()
	jobs := make([]Job, 10)
	for i := range jobs {
		i := i
		jobs[i] = Job{Fetch: func(ctx context.Context) ([]byte, error) {
			return []byte{byte('0' + i)}, nil
		}}
	}
	var progressCalls atomic.Int64
	paths, err := Run(context.Background(), jobs, dir, 4, func(done, total int, bytes int64) {
		if total != 10 {
			t.Errorf("total = %d", total)
		}
		progressCalls.Add(1)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 10 {
		t.Fatalf("paths = %d", len(paths))
	}
	if progressCalls.Load() != 10 {
		t.Fatalf("progress calls = %d", progressCalls.Load())
	}

	dest := filepath.Join(t.TempDir(), "out.ts")
	if err := Concat(paths, dest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "0123456789" {
		t.Fatalf("content = %q", data)
	}
}

func TestRunResumesExistingParts(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path1 := filepath.Join(dir, "000001.part")
	if err := os.WriteFile(path1, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}

	var fetched atomic.Int64
	jobs := []Job{
		{Fetch: func(ctx context.Context) ([]byte, error) { fetched.Add(1); return []byte("a"), nil }},
		{Fetch: func(ctx context.Context) ([]byte, error) { t.Error("part 1 fetched again"); return nil, nil }},
		{Fetch: func(ctx context.Context) ([]byte, error) { fetched.Add(1); return []byte("c"), nil }},
	}
	paths, err := Run(context.Background(), jobs, dir, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Load() != 2 {
		t.Fatalf("fetched = %d, want 2", fetched.Load())
	}
	if len(paths) != 3 {
		t.Fatalf("paths = %d", len(paths))
	}
}

func TestRunPropagatesError(t *testing.T) {
	want := errors.New("boom")
	jobs := []Job{
		{Fetch: func(ctx context.Context) ([]byte, error) { return nil, want }},
	}
	if _, err := Run(context.Background(), jobs, t.TempDir(), 1, nil); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestConcatFailureLeavesNoTemp(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out.bin")
	err := Concat([]string{filepath.Join(t.TempDir(), "missing")}, dest)
	if err == nil {
		t.Fatal("want error")
	}
	if _, statErr := os.Stat(dest + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("temp left behind: %v", statErr)
	}
}
