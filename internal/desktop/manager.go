package desktop

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
	"github.com/neko233-com/DownloadX/internal/inbox"
	"github.com/neko233-com/DownloadX/internal/runner"
)

// Status is a job's state in the queue.
type Status string

// Job states.
const (
	StatusQueued   Status = "queued"
	StatusRunning  Status = "running"
	StatusPaused   Status = "paused"
	StatusDone     Status = "done"
	StatusError    Status = "error"
	StatusCanceled Status = "canceled"
)

// Request is what the GUI asks to download.
type Request struct {
	URL         string
	Dir         string
	Filename    string
	Referrer    string
	Connections int
	Type        engine.Type
}

// Job is one queued or finished download, safe to copy for views.
type Job struct {
	ID          string
	URL         string
	Dir         string
	Filename    string
	Referrer    string
	Connections int
	Type        engine.Type
	Status      Status
	Phase       string
	Message     string
	Done        int64
	Total       int64
	ItemsDone   int
	ItemsTotal  int
	Speed       float64
	Err         string
	Path        string
	Files       []string
	Bytes       int64
	Created     time.Time
	Started     time.Time
	Finished    time.Time

	cancel   context.CancelFunc
	lastDone int64
	lastAt   time.Time
}

// Fraction returns progress between 0 and 1.
func (j Job) Fraction() float64 {
	switch {
	case j.ItemsTotal > 0:
		return clamp01(float64(j.ItemsDone) / float64(j.ItemsTotal))
	case j.Total > 0:
		return clamp01(float64(j.Done) / float64(j.Total))
	case j.Status == StatusDone:
		return 1
	default:
		return 0
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Category groups a job by the kind of file it downloads.
func (j Job) Category() string {
	name := j.Path
	if name == "" {
		name = j.URL
	}
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".zip", ".rar", ".7z", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".zst", ".cab":
		return "compressed"
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".rtf", ".epub", ".mobi", ".csv", ".md":
		return "documents"
	case ".mp3", ".flac", ".wav", ".aac", ".m4a", ".ogg", ".opus", ".wma", ".ape":
		return "music"
	case ".exe", ".msi", ".msix", ".appx", ".apk", ".dmg", ".pkg", ".deb", ".rpm":
		return "programs"
	case ".mp4", ".mkv", ".avi", ".mov", ".webm", ".flv", ".ts", ".m4v", ".wmv", ".mpg", ".mpeg":
		return "video"
	default:
		return "other"
	}
}

// ETA estimates the remaining time at the current speed; zero means unknown.
func (j Job) ETA() time.Duration {
	if j.Speed <= 0 || j.Total <= 0 || j.Done >= j.Total {
		return 0
	}
	return time.Duration(float64(j.Total-j.Done)/j.Speed) * time.Second
}

// Counts summarizes the queue for the status bar.
type Counts struct {
	Total      int
	Active     int
	Queued     int
	Paused     int
	Done       int
	Failed     int
	Speed      float64
	Unfinished int
}

func (m *Manager) Counts() Counts {
	m.mu.Lock()
	defer m.mu.Unlock()
	var c Counts
	for _, j := range m.jobs {
		c.Total++
		switch j.Status {
		case StatusRunning:
			c.Active++
			c.Unfinished++
			c.Speed += j.Speed
		case StatusQueued:
			c.Queued++
			c.Unfinished++
		case StatusPaused:
			c.Paused++
			c.Unfinished++
		case StatusDone:
			c.Done++
		case StatusError:
			c.Failed++
		}
	}
	return c
}

// Manager runs jobs on a small worker pool.
type Manager struct {
	mu           sync.Mutex
	jobs         []*Job
	queue        chan *Job
	rootCtx      context.Context
	stop         context.CancelFunc
	onUpdate     func()
	seq          atomic.Int64
	inboxStarted bool
}

// NewManager starts workers reading the queue.
func NewManager(workers int) *Manager {
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		queue:   make(chan *Job, 64),
		rootCtx: ctx,
		stop:    cancel,
	}
	for i := 0; i < workers; i++ {
		go m.worker()
	}
	return m
}

// SetOnUpdate sets the callback that asks the window for a new frame.
func (m *Manager) SetOnUpdate(fn func()) {
	m.mu.Lock()
	m.onUpdate = fn
	m.mu.Unlock()
}

// StartInbox watches for downloads handed off by the browser extensions and
// queues them into dir. It runs once per manager.
func (m *Manager) StartInbox(dir string) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	m.mu.Lock()
	if m.inboxStarted {
		m.mu.Unlock()
		return
	}
	m.inboxStarted = true
	m.mu.Unlock()

	go m.watchInbox(dir)
}

func (m *Manager) watchInbox(dir string) {
	m.processInbox(dir)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.rootCtx.Done():
			return
		case <-ticker.C:
			m.processInbox(dir)
		}
	}
}

func (m *Manager) processInbox(dir string) {
	items, err := inbox.Pending()
	if err != nil {
		return
	}
	for _, item := range items {
		req := item.Request
		if _, err := m.Add(Request{
			URL:         req.URL,
			Dir:         dir,
			Filename:    req.Filename,
			Referrer:    req.Referrer,
			Connections: engine.DefaultConnections,
			Type:        engine.TypeAuto,
		}); err != nil {
			inbox.Logf("inbox: %v", err)
		}
		inbox.Remove(item.Path)
	}
}

// Stop cancels running jobs and stops the workers.
func (m *Manager) Stop() {
	m.stop()
}

// Add queues a download.
func (m *Manager) Add(req Request) (*Job, error) {
	url := strings.TrimSpace(req.URL)
	if url == "" {
		return nil, errors.New("a URL, magnet link or .torrent path is required")
	}
	conn := req.Connections
	if conn <= 0 {
		conn = engine.DefaultConnections
	}
	if conn > engine.MaxConnections {
		conn = engine.MaxConnections
	}
	typ := req.Type
	if typ == "" {
		typ = engine.TypeAuto
	}
	job := &Job{
		ID:          m.newID(),
		URL:         url,
		Dir:         strings.TrimSpace(req.Dir),
		Filename:    strings.TrimSpace(req.Filename),
		Referrer:    strings.TrimSpace(req.Referrer),
		Connections: conn,
		Type:        typ,
		Status:      StatusQueued,
		Created:     time.Now(),
	}
	m.mu.Lock()
	m.jobs = append(m.jobs, job)
	m.mu.Unlock()

	select {
	case m.queue <- job:
	case <-m.rootCtx.Done():
		return nil, errors.New("download manager stopped")
	}
	m.notify()
	return job, nil
}

// Cancel stops a queued, paused or running job.
func (m *Manager) Cancel(id string) {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.ID != id {
			continue
		}
		switch j.Status {
		case StatusQueued, StatusPaused:
			j.Status = StatusCanceled
		case StatusRunning:
			j.Status = StatusCanceled
			if j.cancel != nil {
				j.cancel()
			}
		}
		break
	}
	m.mu.Unlock()
	m.notify()
}

// Pause stops a running job and keeps its partial files so Resume continues
// where it stopped. A queued job is paused before a worker starts it.
func (m *Manager) Pause(id string) {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.ID != id {
			continue
		}
		switch j.Status {
		case StatusQueued:
			j.Status = StatusPaused
		case StatusRunning:
			j.Status = StatusPaused
			if j.cancel != nil {
				j.cancel()
			}
		}
		break
	}
	m.mu.Unlock()
	m.notify()
}

// Resume queues a paused job again. The engine resumes from the partial file.
func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	var job *Job
	for _, j := range m.jobs {
		if j.ID != id {
			continue
		}
		if j.Status != StatusPaused {
			m.mu.Unlock()
			return errors.New("the download is not paused")
		}
		j.Status = StatusQueued
		j.Finished = time.Time{}
		job = j
		break
	}
	m.mu.Unlock()
	if job == nil {
		return errors.New("download not found")
	}
	select {
	case m.queue <- job:
	case <-m.rootCtx.Done():
		return errors.New("download manager stopped")
	}
	m.notify()
	return nil
}

// PauseAll pauses every running or queued job and returns how many changed.
func (m *Manager) PauseAll() int {
	m.mu.Lock()
	n := 0
	for _, j := range m.jobs {
		switch j.Status {
		case StatusQueued:
			j.Status = StatusPaused
			n++
		case StatusRunning:
			j.Status = StatusPaused
			if j.cancel != nil {
				j.cancel()
			}
			n++
		}
	}
	m.mu.Unlock()
	if n > 0 {
		m.notify()
	}
	return n
}

// ResumeAll queues every paused job and returns how many changed.
func (m *Manager) ResumeAll() int {
	m.mu.Lock()
	var paused []*Job
	for _, j := range m.jobs {
		if j.Status == StatusPaused {
			j.Status = StatusQueued
			j.Finished = time.Time{}
			paused = append(paused, j)
		}
	}
	m.mu.Unlock()
	for _, j := range paused {
		select {
		case m.queue <- j:
		case <-m.rootCtx.Done():
			return len(paused)
		}
	}
	if len(paused) > 0 {
		m.notify()
	}
	return len(paused)
}

// Retry queues the same request again.
func (m *Manager) Retry(id string) error {
	m.mu.Lock()
	var req Request
	found := false
	for _, j := range m.jobs {
		if j.ID != id {
			continue
		}
		if j.Status == StatusRunning {
			m.mu.Unlock()
			return errors.New("the download is still running")
		}
		req = Request{URL: j.URL, Dir: j.Dir, Filename: j.Filename, Referrer: j.Referrer, Connections: j.Connections, Type: j.Type}
		found = true
		break
	}
	m.mu.Unlock()
	if !found {
		return errors.New("download not found")
	}
	_, err := m.Add(req)
	return err
}

// Remove drops a job that is not running.
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	for i, j := range m.jobs {
		if j.ID != id || j.Status == StatusRunning {
			continue
		}
		if j.Status == StatusQueued || j.Status == StatusPaused {
			j.Status = StatusCanceled
		}
		m.jobs = append(m.jobs[:i], m.jobs[i+1:]...)
		break
	}
	m.mu.Unlock()
	m.notify()
}

// ClearFinished removes completed, failed and canceled jobs.
func (m *Manager) ClearFinished() {
	m.mu.Lock()
	kept := m.jobs[:0]
	for _, j := range m.jobs {
		if j.Status == StatusDone || j.Status == StatusError || j.Status == StatusCanceled {
			continue
		}
		kept = append(kept, j)
	}
	m.jobs = kept
	m.mu.Unlock()
	m.notify()
}

// HasFinished reports whether any job can be cleared.
func (m *Manager) HasFinished() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		switch j.Status {
		case StatusDone, StatusError, StatusCanceled:
			return true
		}
	}
	return false
}

// Snapshot copies the jobs for a view.
func (m *Manager) Snapshot() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, len(m.jobs))
	for i, j := range m.jobs {
		out[i] = *j
	}
	return out
}

func (m *Manager) newID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatInt(m.seq.Add(1), 36)
}

func (m *Manager) worker() {
	for {
		select {
		case <-m.rootCtx.Done():
			return
		case job := <-m.queue:
			m.run(job)
		}
	}
}

func (m *Manager) run(job *Job) {
	m.mu.Lock()
	if job.Status != StatusQueued {
		m.mu.Unlock()
		return
	}
	job.Status = StatusRunning
	job.Started = time.Now()
	job.lastAt = time.Now()
	job.lastDone = 0
	job.Speed = 0
	ctx, cancel := context.WithCancel(m.rootCtx)
	job.cancel = cancel
	task := &engine.Task{
		URL:         job.URL,
		Dir:         job.Dir,
		Connections: job.Connections,
		Type:        job.Type,
		Referer:     job.Referrer,
		Timeout:     30 * time.Second,
		Retries:     3,
		HLSQuality:  "best",
		FFmpeg:      "ffmpeg",
	}
	torrent := job.Type == engine.TypeBT ||
		strings.HasPrefix(strings.ToLower(job.URL), "magnet:") ||
		strings.HasSuffix(strings.ToLower(job.URL), ".torrent")
	if job.Filename != "" && !torrent {
		task.Dest = filepath.Join(job.Dir, engine.SanitizeFilename(job.Filename))
	}
	task.Progress = func(p engine.Progress) { m.progress(job, p) }
	m.mu.Unlock()
	m.notify()

	res, err := runner.Run(ctx, task)
	cancel()

	m.mu.Lock()
	job.cancel = nil
	job.Finished = time.Now()
	switch {
	case job.Status == StatusPaused:
		// Pause keeps the partial files and the progress already reported.
		job.Err = ""
		job.Speed = 0
	case job.Status == StatusCanceled || errors.Is(err, context.Canceled):
		job.Status = StatusCanceled
		job.Err = ""
		job.Speed = 0
	case err != nil:
		job.Status = StatusError
		job.Err = err.Error()
		job.Speed = 0
	case res != nil:
		job.Status = StatusDone
		job.Path = res.Path
		job.Files = res.Files
		job.Bytes = res.Bytes
		job.Speed = 0
		if res.Bytes > 0 {
			job.Done = res.Bytes
			job.Total = res.Bytes
		}
	}
	m.mu.Unlock()
	m.notify()
}

func (m *Manager) progress(job *Job, p engine.Progress) {
	m.mu.Lock()
	if job.Status == StatusPaused || job.Status == StatusCanceled {
		m.mu.Unlock()
		return
	}
	job.Phase = p.Phase
	job.Message = p.Message
	if p.Done > 0 || p.Total > 0 {
		job.Done = p.Done
		job.Total = p.Total
	}
	if p.ItemsTotal > 0 {
		job.ItemsDone = p.ItemsDone
		job.ItemsTotal = p.ItemsTotal
	}
	if job.Status == StatusQueued {
		job.Status = StatusRunning
	}

	now := time.Now()
	if job.lastAt.IsZero() {
		job.lastAt = now
		job.lastDone = p.Done
	}
	if p.ItemsTotal == 0 && now.Sub(job.lastAt) >= 300*time.Millisecond {
		dt := now.Sub(job.lastAt).Seconds()
		delta := p.Done - job.lastDone
		if dt > 0 && delta >= 0 {
			instant := float64(delta) / dt
			if job.Speed == 0 {
				job.Speed = instant
			} else {
				job.Speed = job.Speed*0.7 + instant*0.3
			}
		}
		job.lastDone = p.Done
		job.lastAt = now
	}
	m.mu.Unlock()
	m.notify()
}

func (m *Manager) notify() {
	m.mu.Lock()
	cb := m.onUpdate
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// JobStatusText is a human label for a status.
func JobStatusText(s Status) string {
	switch s {
	case StatusQueued:
		return "Queued"
	case StatusRunning:
		return "Downloading"
	case StatusDone:
		return "Done"
	case StatusError:
		return "Failed"
	case StatusCanceled:
		return "Canceled"
	default:
		return fmt.Sprint(s)
	}
}
