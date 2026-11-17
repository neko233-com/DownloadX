package dash

import (
	"bytes"
	"context"
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

type dashTestServer struct {
	files map[string][]byte
	mu    sync.Mutex
	hits  map[string]int
}

func (s *dashTestServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.hits == nil {
		s.hits = map[string]int{}
	}
	s.hits[r.URL.Path]++
	s.mu.Unlock()

	data, ok := s.files[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func newDashTestServer(t *testing.T, files map[string][]byte) (*httptest.Server, *dashTestServer) {
	t.Helper()
	h := &dashTestServer{files: files}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, h
}

func dashTask(srv *httptest.Server, dir string, conns int) *engine.Task {
	return &engine.Task{
		URL:         srv.URL + "/video.mpd",
		Dir:         dir,
		Connections: conns,
		Retries:     0,
		Timeout:     5 * time.Second,
		FFmpeg:      "definitely-not-ffmpeg-12345",
	}
}

const singleTrackMPD = `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT8S">
  <Period>
    <AdaptationSet contentType="video" mimeType="video/mp4">
      <SegmentTemplate timescale="1" duration="2" startNumber="1" media="seg-$Number$.m4s" initialization="init.mp4"/>
      <Representation id="v" bandwidth="1000000" codecs="avc1.4d401f" width="1280" height="720"/>
    </AdaptationSet>
  </Period>
</MPD>`

func TestDownloadSegmentTemplateSingleTrack(t *testing.T) {
	files := map[string][]byte{
		"/video.mpd": []byte(singleTrackMPD),
		"/init.mp4":  []byte("INIT"),
		"/seg-1.m4s": []byte("A"),
		"/seg-2.m4s": []byte("B"),
		"/seg-3.m4s": []byte("C"),
		"/seg-4.m4s": []byte("D"),
	}
	srv, _ := newDashTestServer(t, files)

	res, err := Download(context.Background(), dashTask(srv, t.TempDir(), 4))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if filepath.Base(res.Path) != "video.mp4" {
		t.Fatalf("path = %q", res.Path)
	}
	got, _ := os.ReadFile(res.Path)
	if string(got) != "INITABCD" {
		t.Fatalf("content = %q", got)
	}
	if _, err := os.Stat(engine.PartsDir(res.Path)); !os.IsNotExist(err) {
		t.Errorf("parts dir still present: %v", err)
	}
}

const splitTrackMPD = `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT4S">
  <Period>
    <AdaptationSet contentType="video" mimeType="video/mp4">
      <SegmentTemplate timescale="1" duration="2" startNumber="1" media="v-$Number$.m4s" initialization="vinit.mp4"/>
      <Representation id="v" bandwidth="1000000"/>
    </AdaptationSet>
    <AdaptationSet contentType="audio" mimeType="audio/mp4">
      <SegmentTemplate timescale="1" duration="2" startNumber="1" media="a-$Number$.m4s" initialization="ainit.mp4"/>
      <Representation id="a" bandwidth="128000"/>
    </AdaptationSet>
  </Period>
</MPD>`

func TestDownloadSplitTracksNoFFmpeg(t *testing.T) {
	files := map[string][]byte{
		"/video.mpd": []byte(splitTrackMPD),
		"/vinit.mp4": []byte("VINIT"),
		"/v-1.m4s":   []byte("V1"),
		"/v-2.m4s":   []byte("V2"),
		"/ainit.mp4": []byte("AINIT"),
		"/a-1.m4s":   []byte("A1"),
		"/a-2.m4s":   []byte("A2"),
	}
	srv, _ := newDashTestServer(t, files)

	dir := t.TempDir()
	res, err := Download(context.Background(), dashTask(srv, dir, 4))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if len(res.Files) != 2 {
		t.Fatalf("files = %v", res.Files)
	}
	video := filepath.Join(dir, "video.video.mp4")
	audio := filepath.Join(dir, "video.audio.m4a")
	if res.Files[0] != video || res.Files[1] != audio {
		t.Fatalf("files = %v", res.Files)
	}
	vdata, _ := os.ReadFile(video)
	if string(vdata) != "VINITV1V2" {
		t.Fatalf("video = %q", vdata)
	}
	adata, _ := os.ReadFile(audio)
	if string(adata) != "AINITA1A2" {
		t.Fatalf("audio = %q", adata)
	}
}

func TestDownloadSegmentListRanges(t *testing.T) {
	mpd := `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT4S">
  <Period>
    <AdaptationSet contentType="video" mimeType="video/mp4">
      <Representation id="v" bandwidth="1000">
        <SegmentList>
          <Initialization sourceURL="file.bin" range="0-3"/>
          <SegmentURL media="file.bin" mediaRange="4-7"/>
        </SegmentList>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`
	files := map[string][]byte{
		"/video.mpd": []byte(mpd),
		"/file.bin":  []byte("0123456789"),
	}
	srv, _ := newDashTestServer(t, files)

	res, err := Download(context.Background(), dashTask(srv, t.TempDir(), 2))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, []byte("01234567")) {
		t.Fatalf("content = %q", got)
	}
}

func TestDownloadDRMRejected(t *testing.T) {
	mpd := `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT4S">
  <Period>
    <AdaptationSet contentType="video" mimeType="video/mp4">
      <ContentProtection schemeIdUri="urn:mpeg:dash:mp4protection:2011" value="cenc"/>
      <Representation id="v" bandwidth="1000"/>
    </AdaptationSet>
  </Period>
</MPD>`
	srv, _ := newDashTestServer(t, map[string][]byte{"/video.mpd": []byte(mpd)})
	if _, err := Download(context.Background(), dashTask(srv, t.TempDir(), 2)); err == nil || !strings.Contains(err.Error(), "DRM") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadManifestNotFound(t *testing.T) {
	srv, _ := newDashTestServer(t, map[string][]byte{})
	if _, err := Download(context.Background(), dashTask(srv, t.TempDir(), 2)); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}
