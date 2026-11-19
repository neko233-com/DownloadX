package desktop

import (
	"testing"
	"time"
)

func testJobs() []*Job {
	now := time.Now()
	return []*Job{
		{ID: "a", URL: "https://example.com/movie.mp4", Status: StatusRunning, Total: 100, Done: 50, Speed: 10, Created: now.Add(-3 * time.Minute)},
		{ID: "b", URL: "https://example.com/archive.zip", Status: StatusDone, Total: 200, Done: 200, Created: now.Add(-2 * time.Minute)},
		{ID: "c", URL: "https://example.com/report.pdf", Status: StatusPaused, Total: 300, Done: 150, Created: now.Add(-1 * time.Minute)},
	}
}

func TestVisibleJobsCategoryAndStatusFilters(t *testing.T) {
	app := newTestApp(t)
	defer app.Stop()
	app.manager.jobs = testJobs()

	cases := map[string][]string{
		"all":         {"a", "b", "c"},
		"downloading": {"a"},
		"finished":    {"b"},
		"paused":      {"c"},
		"video":       {"a"},
		"compressed":  {"b"},
		"documents":   {"c"},
		"failed":      {},
	}
	for filter, want := range cases {
		app.filter = filter
		got := app.visibleJobs()
		if len(got) != len(want) {
			t.Fatalf("filter %q returned %d jobs, want %d", filter, len(got), len(want))
		}
		ids := map[string]bool{}
		for _, j := range got {
			ids[j.ID] = true
		}
		for _, id := range want {
			if !ids[id] {
				t.Fatalf("filter %q missing job %s (got %v)", filter, id, ids)
			}
		}
	}
}

func TestVisibleJobsSearchAndSort(t *testing.T) {
	app := newTestApp(t)
	defer app.Stop()
	app.manager.jobs = testJobs()

	app.search = "report"
	if got := app.visibleJobs(); len(got) != 1 || got[0].ID != "c" {
		t.Fatalf("search returned %+v", got)
	}
	app.search = ""

	app.sortBy = "size"
	app.sortAsc = true
	if got := app.visibleJobs(); got[0].ID != "a" || got[2].ID != "c" {
		t.Fatalf("size sort = %v", []string{got[0].ID, got[1].ID, got[2].ID})
	}

	app.sortBy = "added"
	app.sortAsc = false
	if got := app.visibleJobs(); got[0].ID != "c" {
		t.Fatalf("added sort first = %s", got[0].ID)
	}
}

func TestJobETAAndCounts(t *testing.T) {
	j := Job{Total: 1000, Done: 400, Speed: 100}
	if got := j.ETA(); got != 6*time.Second {
		t.Fatalf("ETA = %v", got)
	}
	m := newTestManager()
	m.jobs = testJobs()
	c := m.Counts()
	if c.Total != 3 || c.Active != 1 || c.Done != 1 || c.Paused != 1 || c.Unfinished != 2 {
		t.Fatalf("counts = %+v", c)
	}
}
