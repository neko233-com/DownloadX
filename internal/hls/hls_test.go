package hls

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
)

type hlsStats struct {
	mu       sync.Mutex
	counts   map[string]int
	failPath string
	fail     bool
}

func newHLSStats() *hlsStats {
	return &hlsStats{counts: map[string]int{}}
}

func (s *hlsStats) inc(path string) {
	s.mu.Lock()
	s.counts[path]++
	s.mu.Unlock()
}

func (s *hlsStats) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[path]
}

func (s *hlsStats) setFail(path string, fail bool) {
	s.mu.Lock()
	s.failPath = path
	s.fail = fail
	s.mu.Unlock()
}

func (s *hlsStats) failing(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fail && path == s.failPath
}

type staticServer struct {
	files map[string][]byte
	stats *hlsStats
}

func (s *staticServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	s.stats.inc(p)
	if s.stats.failing(p) {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	data, ok := s.files[p]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func newHLSServer(t *testing.T, files map[string][]byte) (*httptest.Server, *hlsStats) {
	t.Helper()
	stats := newHLSStats()
	srv := httptest.NewServer(&staticServer{files: files, stats: stats})
	t.Cleanup(srv.Close)
	return srv, stats
}

func makeSegments(n int, prefix string) [][]byte {
	segs := make([][]byte, n)
	for i := range segs {
		segs[i] = []byte(fmt.Sprintf("%s-%02d;", prefix, i))
	}
	return segs
}

func buildMediaPlaylist(segs [][]byte, extra string) []byte {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	if extra != "" {
		b.WriteString(extra)
	}
	for i := range segs {
		fmt.Fprintf(&b, "#EXTINF:4.0,\nseg%d.ts\n", i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return []byte(b.String())
}

func segmentFiles(segs [][]byte) map[string][]byte {
	files := map[string][]byte{}
	for i, seg := range segs {
		files[fmt.Sprintf("/seg%d.ts", i)] = seg
	}
	return files
}

func concatBytes(segs [][]byte) []byte {
	var out []byte
	for _, seg := range segs {
		out = append(out, seg...)
	}
	return out
}

func newHLSTask(srv *httptest.Server, path, dir string) *engine.Task {
	return &engine.Task{
		URL:         srv.URL + path,
		Dir:         dir,
		Connections: 4,
		Retries:     0,
		Timeout:     5 * time.Second,
	}
}

func TestDownloadVOD(t *testing.T) {
	segs := makeSegments(10, "seg")
	files := segmentFiles(segs)
	files["/media.m3u8"] = buildMediaPlaylist(segs, "")
	srv, stats := newHLSServer(t, files)

	dir := t.TempDir()
	res, err := Download(context.Background(), newHLSTask(srv, "/media.m3u8", dir))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if filepath.Base(res.Path) != "media.ts" {
		t.Fatalf("path = %q", res.Path)
	}
	got, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, concatBytes(segs)) {
		t.Fatal("content mismatch")
	}
	if stats.count("/seg0.ts") != 1 {
		t.Errorf("segment 0 fetched %d times", stats.count("/seg0.ts"))
	}
	if _, err := os.Stat(engine.PartsDir(res.Path)); !os.IsNotExist(err) {
		t.Errorf("parts dir still present: %v", err)
	}
}

func TestDownloadMasterQuality(t *testing.T) {
	lowSegs := makeSegments(3, "low")
	highSegs := makeSegments(3, "high")

	files := map[string][]byte{
		"/master.m3u8": []byte("#EXTM3U\n" +
			"#EXT-X-STREAM-INF:BANDWIDTH=100000\nlow.m3u8\n" +
			"#EXT-X-STREAM-INF:BANDWIDTH=900000\nhigh.m3u8\n"),
		"/low.m3u8": []byte("#EXTM3U\n" +
			"#EXTINF:4.0,\nseg0.ts\n#EXTINF:4.0,\nseg1.ts\n#EXTINF:4.0,\nseg2.ts\n#EXT-X-ENDLIST\n"),
		"/high.m3u8": []byte("#EXTM3U\n" +
			"#EXTINF:4.0,\nhigh0.ts\n#EXTINF:4.0,\nhigh1.ts\n#EXTINF:4.0,\nhigh2.ts\n#EXT-X-ENDLIST\n"),
	}
	for i, seg := range lowSegs {
		files[fmt.Sprintf("/seg%d.ts", i)] = seg
	}
	for i, seg := range highSegs {
		files[fmt.Sprintf("/high%d.ts", i)] = seg
	}

	srv, _ := newHLSServer(t, files)

	// Default best -> high variant.
	res, err := Download(context.Background(), newHLSTask(srv, "/master.m3u8", t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, concatBytes(highSegs)) {
		t.Fatal("best quality content mismatch")
	}

	// Explicit worst -> low variant.
	task := newHLSTask(srv, "/master.m3u8", t.TempDir())
	task.HLSQuality = "worst"
	res, err = Download(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(res.Path)
	if !bytes.Equal(got, concatBytes(lowSegs)) {
		t.Fatal("worst quality content mismatch")
	}
}

func TestDownloadAES128(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := makeSegments(4, "plain")
	encrypted := make([][]byte, len(plain))
	for i, seg := range plain {
		iv := make([]byte, 16)
		binary.BigEndian.PutUint64(iv[8:], uint64(i))
		encrypted[i] = encryptAES128(key, iv, seg)
	}

	files := segmentFiles(encrypted)
	files["/key.bin"] = key
	files["/media.m3u8"] = buildMediaPlaylist(encrypted, "#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n")
	srv, _ := newHLSServer(t, files)

	res, err := Download(context.Background(), newHLSTask(srv, "/media.m3u8", t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, concatBytes(plain)) {
		t.Fatal("decrypted content mismatch")
	}
}

func TestDownloadByteRange(t *testing.T) {
	files := map[string][]byte{
		"/seg0.ts":    []byte("0123456789"),
		"/media.m3u8": []byte("#EXTM3U\n#EXTINF:4.0,\n#EXT-X-BYTERANGE:5@2\nseg0.ts\n#EXT-X-ENDLIST\n"),
	}
	srv, _ := newHLSServer(t, files)

	res, err := Download(context.Background(), newHLSTask(srv, "/media.m3u8", t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(res.Path)
	if string(got) != "23456" {
		t.Fatalf("content = %q", got)
	}
}

func TestDownloadInitMap(t *testing.T) {
	files := map[string][]byte{
		"/init.mp4": []byte("INIT"),
		"/seg0.m4s": []byte("-A-"),
		"/seg1.m4s": []byte("-B-"),
		"/media.m3u8": []byte("#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n" +
			"#EXTINF:4.0,\nseg0.m4s\n#EXTINF:4.0,\nseg1.m4s\n#EXT-X-ENDLIST\n"),
	}
	srv, _ := newHLSServer(t, files)

	res, err := Download(context.Background(), newHLSTask(srv, "/media.m3u8", t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(res.Path) != "media.mp4" {
		t.Fatalf("path = %q", res.Path)
	}
	got, _ := os.ReadFile(res.Path)
	if string(got) != "INIT-A--B-" {
		t.Fatalf("content = %q", got)
	}
}

func TestDownloadResume(t *testing.T) {
	segs := makeSegments(8, "seg")
	files := segmentFiles(segs)
	files["/media.m3u8"] = buildMediaPlaylist(segs, "")
	srv, stats := newHLSServer(t, files)
	stats.setFail("/seg5.ts", true)

	dir := t.TempDir()
	task := newHLSTask(srv, "/media.m3u8", dir)
	if _, err := Download(context.Background(), task); err == nil {
		t.Fatal("want failure while segment 5 is failing")
	}
	partsDir := engine.PartsDir(filepath.Join(dir, "media.ts"))
	if _, err := os.Stat(partsDir); err != nil {
		t.Fatalf("parts dir missing: %v", err)
	}
	if stats.count("/seg0.ts") == 0 {
		t.Fatal("segment 0 was never fetched")
	}
	before := stats.count("/seg0.ts")

	stats.setFail("/seg5.ts", false)
	res, err := Download(context.Background(), task)
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, concatBytes(segs)) {
		t.Fatal("content mismatch after resume")
	}
	if stats.count("/seg0.ts") != before {
		t.Errorf("segment 0 fetched again after resume")
	}
}

func TestDownloadLiveRejected(t *testing.T) {
	files := map[string][]byte{
		"/media.m3u8": []byte("#EXTM3U\n#EXTINF:4.0,\nseg0.ts\n"),
		"/seg0.ts":    []byte("x"),
	}
	srv, _ := newHLSServer(t, files)
	_, err := Download(context.Background(), newHLSTask(srv, "/media.m3u8", t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "live") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadDRMRejected(t *testing.T) {
	files := map[string][]byte{
		"/media.m3u8": []byte("#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key.bin\"\n" +
			"#EXTINF:4.0,\nseg0.ts\n#EXT-X-ENDLIST\n"),
		"/seg0.ts": []byte("x"),
		"/key.bin": []byte("0123456789abcdef"),
	}
	srv, _ := newHLSServer(t, files)
	_, err := Download(context.Background(), newHLSTask(srv, "/media.m3u8", t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "unsupported encryption") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseIV(t *testing.T) {
	iv, err := parseIV("", 258)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint64(iv[8:]) != 258 {
		t.Fatalf("iv = %x", iv)
	}
	iv, err = parseIV("0x00000000000000000000000000000001", 0)
	if err != nil || iv[15] != 1 {
		t.Fatalf("iv = %x err = %v", iv, err)
	}
	if _, err := parseIV("0x12", 0); err == nil {
		t.Fatal("want bad IV length error")
	}
}

func encryptAES128(key, iv, plain []byte) []byte {
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := make([]byte, len(plain)+pad)
	copy(padded, plain)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(pad)
	}
	block, _ := aes.NewCipher(key)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out
}
