package desktop

import (
	"context"
	"testing"
)

func newTestManager() *Manager {
	return &Manager{
		queue:   make(chan *Job, 8),
		rootCtx: context.Background(),
		stop:    func() {},
	}
}

func TestPauseResumeTransitions(t *testing.T) {
	m := newTestManager()
	job := &Job{ID: "a", Status: StatusQueued}
	m.jobs = []*Job{job}

	m.Pause("a")
	if job.Status != StatusPaused {
		t.Fatalf("status = %q, want paused", job.Status)
	}
	if err := m.Resume("a"); err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusQueued {
		t.Fatalf("status = %q, want queued", job.Status)
	}
	select {
	case got := <-m.queue:
		if got != job {
			t.Fatal("resume queued another job")
		}
	default:
		t.Fatal("resume did not queue the job")
	}

	m.Pause("a")
	if n := m.PauseAll(); n != 0 {
		t.Fatalf("PauseAll changed %d already paused jobs", n)
	}
	m.ResumeAll()
	if job.Status != StatusQueued {
		t.Fatalf("ResumeAll left status %q", job.Status)
	}
	m.Cancel("a")
	if job.Status != StatusCanceled {
		t.Fatalf("cancel left status %q", job.Status)
	}
}

func TestJobCategory(t *testing.T) {
	cases := map[string]string{
		"https://example.com/a.zip": "compressed",
		"https://example.com/a.pdf": "documents",
		"https://example.com/a.mp3": "music",
		"https://example.com/a.exe": "programs",
		"https://example.com/a.mp4": "video",
		"https://example.com/a.iso": "other",
		"magnet:?xt=urn:btih:abc":   "other",
	}
	for raw, want := range cases {
		j := Job{URL: raw}
		if got := j.Category(); got != want {
			t.Errorf("Category(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestManagerCounts(t *testing.T) {
	m := newTestManager()
	m.jobs = []*Job{
		{Status: StatusRunning, Speed: 100},
		{Status: StatusQueued},
		{Status: StatusPaused},
		{Status: StatusDone},
		{Status: StatusError},
	}
	c := m.Counts()
	if c.Total != 5 || c.Active != 1 || c.Queued != 1 || c.Paused != 1 || c.Done != 1 || c.Failed != 1 || c.Unfinished != 3 {
		t.Fatalf("counts = %+v", c)
	}
	if c.Speed != 100 {
		t.Fatalf("speed = %v", c.Speed)
	}
}
