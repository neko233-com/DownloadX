// Package engine defines the shared task model used by every protocol
// downloader, plus destination naming, resume state and rate limiting.
package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
)

// Type selects the download protocol.
type Type string

// Supported download types.
const (
	TypeAuto Type = "auto"
	TypeHTTP Type = "http"
	TypeFTP  Type = "ftp"
	TypeHLS  Type = "hls"
	TypeDASH Type = "dash"
	TypeBT   Type = "bt"
)

// ParseType validates a --type value.
func ParseType(s string) (Type, error) {
	t := Type(strings.ToLower(strings.TrimSpace(s)))
	switch t {
	case TypeAuto, TypeHTTP, TypeFTP, TypeHLS, TypeDASH, TypeBT:
		return t, nil
	}
	return "", fmt.Errorf("unknown type %q (want auto, http, ftp, hls, dash or bt)", s)
}

// Defaults and limits shared by the CLI and downloaders.
const (
	DefaultConnections = 16
	MaxConnections     = 128
	MinSegmentSize     = 512 * 1024
)

// Task describes one download.
type Task struct {
	URL         string
	Dest        string // explicit output file or directory; empty = infer
	Dir         string // destination directory when Dest is empty
	Connections int
	Headers     map[string]string
	Cookie      string
	Referer     string
	UserAgent   string
	Username    string
	Password    string
	Proxy       string
	Insecure    bool
	Timeout     time.Duration
	Retries     int
	NoResume    bool
	Limit       int64 // bytes per second; 0 means unlimited
	Type        Type
	HLSQuality  string
	BTPort      int
	Seed        bool
	FFmpeg      string
	Progress    func(Progress)
}

// ConnectionCount returns the effective connection count clamped to sane limits.
func (t *Task) ConnectionCount() int {
	n := t.Connections
	if n <= 0 {
		n = DefaultConnections
	}
	if n > MaxConnections {
		n = MaxConnections
	}
	return n
}

// Progress is the single progress shape every downloader reports.
type Progress struct {
	Phase       string
	Done        int64
	Total       int64
	ItemsDone   int
	ItemsTotal  int
	Connections int
	Message     string
}

// Result describes a finished download.
type Result struct {
	Path  string
	Bytes int64
	Files []string
}

// Downloader is implemented by every protocol package.
type Downloader interface {
	Download(ctx context.Context, t *Task) (*Result, error)
}

// Sentinel errors shared across packages.
var (
	ErrInterrupted  = errors.New("interrupted")
	ErrUnsupported  = errors.New("unsupported source")
	ErrRangeIgnored = errors.New("server ignored Range request")
)

// DetectType guesses a protocol from the source string alone. It never
// performs network I/O; the runner refines HTTP results with a probe.
func DetectType(raw string) Type {
	lower := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(lower, "magnet:") {
		return TypeBT
	}
	u, err := url.Parse(raw)
	if err != nil {
		return TypeAuto
	}
	switch strings.ToLower(u.Scheme) {
	case "ftp", "ftps":
		return TypeFTP
	case "http", "https":
		switch strings.ToLower(path.Ext(u.Path)) {
		case ".m3u8":
			return TypeHLS
		case ".mpd":
			return TypeDASH
		case ".torrent":
			return TypeBT
		}
	}
	return TypeAuto
}
