// Package hls parses HLS playlists and downloads the video/audio stream.
package hls

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Playlist is a parsed master or media playlist.
type Playlist struct {
	Variants []Variant
	Segments []Segment
	EndList  bool
}

// IsMaster reports whether this is a multivariant (master) playlist.
func (p *Playlist) IsMaster() bool { return len(p.Variants) > 0 }

// Variant is one rendition listed in a master playlist.
type Variant struct {
	URI        string
	Bandwidth  int64
	Resolution string
	Codecs     string
}

// ByteRange is an EXT-X-BYTERANGE sub-range.
type ByteRange struct {
	Length    int64
	Offset    int64
	HasOffset bool
}

// Key is an EXT-X-KEY encryption declaration.
type Key struct {
	Method string
	URI    string
	IV     string
}

// Map is an EXT-X-MAP initialization segment.
type Map struct {
	URI       string
	ByteRange *ByteRange
}

// Segment is one media segment.
type Segment struct {
	URI       string
	Duration  float64
	Sequence  int64
	Key       *Key
	Map       *Map
	ByteRange *ByteRange
}

// Parse parses data as a playlist relative to baseURL.
func Parse(baseURL string, data []byte) (*Playlist, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("hls: bad playlist URL: %w", err)
	}
	resolve := func(ref string) string {
		if ref == "" {
			return ""
		}
		u, err := url.Parse(ref)
		if err != nil {
			return ref
		}
		return base.ResolveReference(u).String()
	}

	p := &Playlist{}
	var (
		pendingVariant map[string]string
		pendingDur     float64
		hasDur         bool
		pendingRange   *ByteRange
		curKey         *Key
		curMap         *Map
		seq            int64
		lastRangeEnd   int64
	)

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			pendingVariant = parseAttributes(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			if n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:")), 10, 64); err == nil {
				seq = n
			}
		case strings.HasPrefix(line, "#EXTINF:"):
			v := strings.TrimPrefix(line, "#EXTINF:")
			if i := strings.IndexByte(v, ','); i >= 0 {
				v = v[:i]
			}
			d, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return nil, fmt.Errorf("hls: bad EXTINF %q", line)
			}
			pendingDur = d
			hasDur = true
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			attrs := parseAttributes(strings.TrimPrefix(line, "#EXT-X-KEY:"))
			if strings.EqualFold(attrs["METHOD"], "NONE") {
				curKey = nil
				continue
			}
			curKey = &Key{Method: attrs["METHOD"], URI: resolve(attrs["URI"]), IV: attrs["IV"]}
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			attrs := parseAttributes(strings.TrimPrefix(line, "#EXT-X-MAP:"))
			curMap = &Map{URI: resolve(attrs["URI"])}
			if br := attrs["BYTERANGE"]; br != "" {
				curMap.ByteRange = parseByteRange(br, 0)
			}
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
			pendingRange = parseByteRange(strings.TrimPrefix(line, "#EXT-X-BYTERANGE:"), lastRangeEnd)
		case line == "#EXT-X-ENDLIST":
			p.EndList = true
		case strings.HasPrefix(line, "#"):
			// Other tags do not affect the download plan.
		default:
			if pendingVariant != nil {
				bw, _ := strconv.ParseInt(strings.TrimSpace(pendingVariant["BANDWIDTH"]), 10, 64)
				p.Variants = append(p.Variants, Variant{
					URI:        resolve(line),
					Bandwidth:  bw,
					Resolution: pendingVariant["RESOLUTION"],
					Codecs:     pendingVariant["CODECS"],
				})
				pendingVariant = nil
				continue
			}
			if !hasDur {
				continue
			}
			p.Segments = append(p.Segments, Segment{
				URI:       resolve(line),
				Duration:  pendingDur,
				Sequence:  seq,
				Key:       curKey,
				Map:       curMap,
				ByteRange: pendingRange,
			})
			seq++
			if pendingRange != nil {
				lastRangeEnd = pendingRange.Offset + pendingRange.Length
			}
			pendingRange = nil
			pendingDur = 0
			hasDur = false
		}
	}

	if len(p.Variants) == 0 && len(p.Segments) == 0 {
		return nil, fmt.Errorf("hls: playlist contains no variants or segments")
	}
	if len(p.Variants) > 0 && len(p.Segments) > 0 {
		return nil, fmt.Errorf("hls: playlist mixes master and media entries")
	}
	return p, nil
}

// PickVariant selects a rendition: best (default), worst, or a 0-based index.
func (p *Playlist) PickVariant(quality string) (*Variant, error) {
	if len(p.Variants) == 0 {
		return nil, fmt.Errorf("hls: no variants in playlist")
	}
	q := strings.ToLower(strings.TrimSpace(quality))
	if q == "" || q == "best" {
		best := &p.Variants[0]
		for i := 1; i < len(p.Variants); i++ {
			if p.Variants[i].Bandwidth > best.Bandwidth {
				best = &p.Variants[i]
			}
		}
		return best, nil
	}
	if q == "worst" {
		worst := &p.Variants[0]
		for i := 1; i < len(p.Variants); i++ {
			if p.Variants[i].Bandwidth < worst.Bandwidth {
				worst = &p.Variants[i]
			}
		}
		return worst, nil
	}
	n, err := strconv.Atoi(q)
	if err != nil || n < 0 || n >= len(p.Variants) {
		return nil, fmt.Errorf("hls: variant index %q out of range (0-%d)", quality, len(p.Variants)-1)
	}
	return &p.Variants[n], nil
}

func parseAttributes(s string) map[string]string {
	attrs := map[string]string{}
	var items []string
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				items = append(items, s[start:i])
				start = i + 1
			}
		}
	}
	items = append(items, s[start:])

	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		k = strings.ToUpper(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = v[1 : len(v)-1]
		}
		attrs[k] = v
	}
	return attrs
}

func parseByteRange(s string, defaultOffset int64) *ByteRange {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if i := strings.IndexByte(s, '@'); i >= 0 {
		length, err1 := strconv.ParseInt(strings.TrimSpace(s[:i]), 10, 64)
		offset, err2 := strconv.ParseInt(strings.TrimSpace(s[i+1:]), 10, 64)
		if err1 != nil || err2 != nil || length < 0 || offset < 0 {
			return nil
		}
		return &ByteRange{Length: length, Offset: offset, HasOffset: true}
	}
	length, err := strconv.ParseInt(s, 10, 64)
	if err != nil || length < 0 {
		return nil
	}
	return &ByteRange{Length: length, Offset: defaultOffset}
}
