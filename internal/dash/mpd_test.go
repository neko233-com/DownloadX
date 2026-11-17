package dash

import (
	"strings"
	"testing"
)

const sampleMPD = `<?xml version="1.0" encoding="UTF-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT12S">
  <BaseURL>https://cdn.example.com/v/</BaseURL>
  <Period>
    <AdaptationSet contentType="video" mimeType="video/mp4">
      <SegmentTemplate timescale="1000" media="chunk-$Number%03d$-$Time$.m4s" initialization="init.mp4" startNumber="1">
        <SegmentTimeline>
          <S t="0" d="2000" r="1"/>
          <S t="8000" d="2000"/>
        </SegmentTimeline>
      </SegmentTemplate>
      <Representation id="v1" bandwidth="800000" codecs="avc1.4d401f" width="1280" height="720"/>
    </AdaptationSet>
    <AdaptationSet contentType="audio" mimeType="audio/mp4">
      <SegmentTemplate timescale="1000" duration="4000" media="a-$Number$.m4s" initialization="ainit.mp4"/>
      <Representation id="audio-hi" bandwidth="192000" codecs="mp4a.40.2"/>
    </AdaptationSet>
  </Period>
</MPD>`

func TestParseDuration(t *testing.T) {
	cases := map[string]float64{
		"PT12S":      12,
		"PT1H2M3.5S": 3723.5,
		"P1DT1H":     90000,
		"":           0,
		"nonsense":   0,
	}
	for in, want := range cases {
		if got := parseDuration(in); got != want {
			t.Errorf("parseDuration(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseTracks(t *testing.T) {
	m, err := Parse("https://example.com/manifest.mpd", []byte(sampleMPD))
	if err != nil {
		t.Fatal(err)
	}
	video, audio, err := m.PickTracks()
	if err != nil {
		t.Fatal(err)
	}
	if video == nil || video.ID != "v1" {
		t.Fatalf("video = %+v", video)
	}
	if audio == nil || audio.ID != "audio-hi" {
		t.Fatalf("audio = %+v", audio)
	}
	if video.Duration != 12 {
		t.Errorf("duration = %v", video.Duration)
	}

	plan, err := video.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Init == nil || plan.Init.URL != "https://cdn.example.com/v/init.mp4" {
		t.Fatalf("init = %+v", plan.Init)
	}
	want := []string{
		"https://cdn.example.com/v/chunk-001-0.m4s",
		"https://cdn.example.com/v/chunk-002-2000.m4s",
		"https://cdn.example.com/v/chunk-003-8000.m4s",
	}
	if len(plan.Segments) != len(want) {
		t.Fatalf("segments = %+v", plan.Segments)
	}
	for i := range want {
		if plan.Segments[i].URL != want[i] {
			t.Errorf("segment %d = %q, want %q", i, plan.Segments[i].URL, want[i])
		}
	}

	aplan, err := audio.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if len(aplan.Segments) != 3 {
		t.Fatalf("audio segments = %d", len(aplan.Segments))
	}
	if aplan.Segments[0].URL != "https://cdn.example.com/v/a-1.m4s" {
		t.Errorf("audio segment = %q", aplan.Segments[0].URL)
	}
}

func TestPickTracksBest(t *testing.T) {
	mpd := `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT10S">
  <Period>
    <AdaptationSet contentType="video">
      <Representation id="v-low" bandwidth="200000"/>
      <Representation id="v-high" bandwidth="2000000"/>
    </AdaptationSet>
    <AdaptationSet contentType="audio">
      <Representation id="a-low" bandwidth="64000"/>
      <Representation id="a-high" bandwidth="128000"/>
    </AdaptationSet>
    <AdaptationSet contentType="text" mimeType="text/vtt">
      <Representation id="subs" bandwidth="1000"/>
    </AdaptationSet>
  </Period>
</MPD>`
	m, err := Parse("https://example.com/m.mpd", []byte(mpd))
	if err != nil {
		t.Fatal(err)
	}
	video, audio, err := m.PickTracks()
	if err != nil {
		t.Fatal(err)
	}
	if video.ID != "v-high" || audio.ID != "a-high" {
		t.Fatalf("picked %s / %s", video.ID, audio.ID)
	}
}

func TestSegmentListPlan(t *testing.T) {
	mpd := `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT6S">
  <Period>
    <AdaptationSet contentType="video" mimeType="video/mp4">
      <BaseURL>video/</BaseURL>
      <Representation id="v" bandwidth="1000">
        <BaseURL>v.mp4</BaseURL>
        <SegmentList>
          <Initialization sourceURL="init.mp4" range="0-99"/>
          <SegmentURL media="seg1.m4s" mediaRange="0-499"/>
          <SegmentURL media="seg2.m4s"/>
        </SegmentList>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`
	m, err := Parse("https://example.com/x/manifest.mpd", []byte(mpd))
	if err != nil {
		t.Fatal(err)
	}
	tracks, err := m.Tracks()
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 {
		t.Fatalf("tracks = %d", len(tracks))
	}
	plan, err := tracks[0].Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Init.URL != "https://example.com/x/video/init.mp4" || plan.Init.Range.Start != 0 || plan.Init.Range.End != 99 {
		t.Fatalf("init = %+v", plan.Init)
	}
	if len(plan.Segments) != 2 {
		t.Fatalf("segments = %+v", plan.Segments)
	}
	if plan.Segments[0].URL != "https://example.com/x/video/seg1.m4s" || plan.Segments[0].Range.End != 499 {
		t.Errorf("seg0 = %+v", plan.Segments[0])
	}
	if plan.Segments[1].Range != nil {
		t.Errorf("seg1 range = %+v", plan.Segments[1].Range)
	}
}

func TestSegmentBasePlan(t *testing.T) {
	mpd := `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT6S">
  <Period>
    <AdaptationSet contentType="video" mimeType="video/mp4">
      <Representation id="v" bandwidth="1000">
        <BaseURL>movie.mp4</BaseURL>
        <SegmentBase>
          <Initialization sourceURL="movie-init.mp4" range="0-100"/>
        </SegmentBase>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`
	m, err := Parse("https://example.com/dir/m.mpd", []byte(mpd))
	if err != nil {
		t.Fatal(err)
	}
	tracks, _ := m.Tracks()
	plan, err := tracks[0].Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Init.URL != "https://example.com/dir/movie-init.mp4" || plan.Init.Range.End != 100 {
		t.Fatalf("init = %+v", plan.Init)
	}
	if len(plan.Segments) != 1 || plan.Segments[0].URL != "https://example.com/dir/movie.mp4" {
		t.Fatalf("segments = %+v", plan.Segments)
	}
}

func TestDRMRejected(t *testing.T) {
	mpd := `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT6S">
  <Period>
    <AdaptationSet contentType="video" mimeType="video/mp4">
      <ContentProtection schemeIdUri="urn:mpeg:dash:mp4protection:2011" value="cenc"/>
      <Representation id="v" bandwidth="1000"/>
    </AdaptationSet>
  </Period>
</MPD>`
	m, err := Parse("https://example.com/m.mpd", []byte(mpd))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.PickTracks(); err == nil || !strings.Contains(err.Error(), "DRM") {
		t.Fatalf("err = %v", err)
	}
}

func TestDynamicRejected(t *testing.T) {
	mpd := `<?xml version="1.0"?>
<MPD type="dynamic" mediaPresentationDuration="PT6S">
  <Period/>
</MPD>`
	if _, err := Parse("https://example.com/live.mpd", []byte(mpd)); err == nil || !strings.Contains(err.Error(), "dynamic") {
		t.Fatalf("err = %v", err)
	}
}

func TestExpandTemplateErrors(t *testing.T) {
	tr := &Track{ID: "v", Bandwidth: 1000}
	if _, err := expandTemplate("x-$Unknown$.m4s", tr, 1, 0); err == nil {
		t.Fatal("want unknown variable error")
	}
	if _, err := expandTemplate("x-$Number", tr, 1, 0); err == nil {
		t.Fatal("want unmatched $ error")
	}
	if _, err := expandTemplate("x-$Number%s$.m4s", tr, 1, 0); err == nil {
		t.Fatal("want bad format error")
	}
	got, err := expandTemplate("$$-$Number%04d$.m4s", tr, 7, 0)
	if err != nil || got != "$-0007.m4s" {
		t.Fatalf("got %q err %v", got, err)
	}
}
