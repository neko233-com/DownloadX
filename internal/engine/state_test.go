package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "file.bin")
	st := &State{
		URL:          "https://example.com/file.bin",
		Size:         100,
		ETag:         `"abc"`,
		LastModified: "Mon, 01 Jan 2026 00:00:00 GMT",
		Segments: []Segment{
			{Start: 0, End: 49, Done: 50},
			{Start: 50, End: 99, Done: 10},
		},
	}
	if err := SaveState(dest, st); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != st.URL || got.Size != st.Size || got.ETag != st.ETag {
		t.Fatalf("loaded %+v", got)
	}
	if got.DoneBytes() != 60 {
		t.Errorf("DoneBytes = %d, want 60", got.DoneBytes())
	}
	if got.Complete() {
		t.Error("Complete = true, want false")
	}
	got.Segments[0].Done = 50
	got.Segments[1].Done = 50
	if !got.Complete() {
		t.Error("Complete = false, want true")
	}
}

func TestSaveStateOverwrites(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "file.bin")
	if err := SaveState(dest, &State{Size: 1}); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(dest, &State{Size: 2}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Size != 2 {
		t.Fatalf("Size = %d, want 2", got.Size)
	}
}

func TestRemoveState(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "file.bin")
	if err := SaveState(dest, &State{Size: 1}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveState(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(StatePath(dest)); !os.IsNotExist(err) {
		t.Fatalf("state still exists: %v", err)
	}
	if err := RemoveState(dest); err != nil {
		t.Fatalf("second remove: %v", err)
	}
}
