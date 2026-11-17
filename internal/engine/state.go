package engine

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"
)

const stateVersion = 1

// Segment is one contiguous byte range of a file being downloaded.
// End is inclusive, matching HTTP Content-Range semantics.
type Segment struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  int64 `json:"done"`
}

// Len returns the segment size in bytes.
func (s Segment) Len() int64 {
	if s.End < s.Start {
		return 0
	}
	return s.End - s.Start + 1
}

// State is the resume checkpoint stored in <dest>.downloadx.
type State struct {
	Version      int       `json:"version"`
	URL          string    `json:"url"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"lastModified,omitempty"`
	Segments     []Segment `json:"segments"`
	Updated      time.Time `json:"updated"`
}

// DoneBytes returns the number of bytes already written.
func (s *State) DoneBytes() int64 {
	if s == nil {
		return 0
	}
	var n int64
	for _, seg := range s.Segments {
		n += seg.Done
	}
	return n
}

// Complete reports whether every segment is fully downloaded.
func (s *State) Complete() bool {
	if s == nil || len(s.Segments) == 0 {
		return false
	}
	for _, seg := range s.Segments {
		if seg.Done < seg.Len() {
			return false
		}
	}
	return true
}

// LoadState reads the resume state for dest.
func LoadState(dest string) (*State, error) {
	data, err := os.ReadFile(StatePath(dest))
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if st.Version != stateVersion {
		return nil, errors.New("unsupported resume state version")
	}
	return &st, nil
}

// SaveState atomically writes the resume state for dest.
func SaveState(dest string, st *State) error {
	st.Version = stateVersion
	st.Updated = time.Now().UTC()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	path := StatePath(dest)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RemoveState deletes the resume state for dest, ignoring a missing file.
func RemoveState(dest string) error {
	err := os.Remove(StatePath(dest))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
