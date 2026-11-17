package hls

import (
	"strings"
	"testing"
)

const masterPlaylist = `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1280000,RESOLUTION=640x360,CODECS="avc1.4d401e,mp4a.40.2"
low/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2560000,RESOLUTION=1280x720
high/index.m3u8
`

func TestParseMaster(t *testing.T) {
	p, err := Parse("https://example.com/vod/master.m3u8", []byte(masterPlaylist))
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsMaster() || len(p.Variants) != 2 {
		t.Fatalf("variants = %+v", p.Variants)
	}
	if p.Variants[0].URI != "https://example.com/vod/low/index.m3u8" {
		t.Errorf("uri = %q", p.Variants[0].URI)
	}
	if p.Variants[1].Bandwidth != 2560000 {
		t.Errorf("bandwidth = %d", p.Variants[1].Bandwidth)
	}

	best, err := p.PickVariant("best")
	if err != nil || best.Bandwidth != 2560000 {
		t.Fatalf("best = %+v err = %v", best, err)
	}
	worst, err := p.PickVariant("worst")
	if err != nil || worst.Bandwidth != 1280000 {
		t.Fatalf("worst = %+v err = %v", worst, err)
	}
	idx, err := p.PickVariant("1")
	if err != nil || idx.Bandwidth != 2560000 {
		t.Fatalf("index = %+v err = %v", idx, err)
	}
	if _, err := p.PickVariant("5"); err == nil {
		t.Fatal("want range error")
	}
}

const mediaPlaylist = `#EXTM3U
#EXT-X-MEDIA-SEQUENCE:7
#EXT-X-KEY:METHOD=AES-128,URI="key.bin",IV=0x00000000000000000000000000000001
#EXT-X-MAP:URI="init.mp4",BYTERANGE="720@0"
#EXTINF:4.0,
seg0.m4s
#EXT-X-BYTERANGE:1000@500
#EXTINF:4.0,title
seg1.m4s
#EXT-X-KEY:METHOD=NONE
#EXTINF:2.5,
seg2.m4s
#EXT-X-ENDLIST
`

func TestParseMedia(t *testing.T) {
	p, err := Parse("https://example.com/vod/media/index.m3u8", []byte(mediaPlaylist))
	if err != nil {
		t.Fatal(err)
	}
	if p.IsMaster() || len(p.Segments) != 3 || !p.EndList {
		t.Fatalf("playlist = %+v", p)
	}
	s0 := p.Segments[0]
	if s0.Sequence != 7 || s0.URI != "https://example.com/vod/media/seg0.m4s" {
		t.Errorf("seg0 = %+v", s0)
	}
	if s0.Key == nil || s0.Key.Method != "AES-128" || s0.Key.URI != "https://example.com/vod/media/key.bin" {
		t.Errorf("key = %+v", s0.Key)
	}
	if s0.Map == nil || s0.Map.URI != "https://example.com/vod/media/init.mp4" {
		t.Errorf("map = %+v", s0.Map)
	}
	if s0.Map.ByteRange == nil || s0.Map.ByteRange.Length != 720 {
		t.Errorf("map range = %+v", s0.Map.ByteRange)
	}
	s1 := p.Segments[1]
	if s1.ByteRange == nil || s1.ByteRange.Length != 1000 || s1.ByteRange.Offset != 500 {
		t.Errorf("seg1 range = %+v", s1.ByteRange)
	}
	s2 := p.Segments[2]
	if s2.Key != nil {
		t.Errorf("seg2 should have no key: %+v", s2.Key)
	}
	if s2.Sequence != 9 || s2.Duration != 2.5 {
		t.Errorf("seg2 = %+v", s2)
	}
}

func TestParseRejectsLiveAndEmpty(t *testing.T) {
	live := strings.Replace(mediaPlaylist, "#EXT-X-ENDLIST\n", "", 1)
	p, err := Parse("https://example.com/live.m3u8", []byte(live))
	if err != nil {
		t.Fatal(err)
	}
	if p.EndList {
		t.Error("EndList = true for live playlist")
	}
	if _, err := Parse("https://example.com/empty.m3u8", []byte("#EXTM3U\n")); err == nil {
		t.Fatal("want error for empty playlist")
	}
}
