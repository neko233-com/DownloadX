// Package progress renders download progress for terminals, plain logs and
// JSON-lines consumers.
package progress

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
)

// Mode selects how progress is written.
type Mode int

// Reporting modes.
const (
	ModeAuto Mode = iota
	ModeTTY
	ModePlain
	ModeJSON
	ModeQuiet
)

// Reporter renders progress for one task.
type Reporter struct {
	mu        sync.Mutex
	out       io.Writer
	mode      Mode
	label     string
	started   time.Time
	last      time.Time
	lastPrint time.Time
	lastDone  int64
	speed     float64
	spinner   int
}

// New creates a reporter. ModeAuto picks TTY when out is a terminal.
func New(out io.Writer, mode Mode) *Reporter {
	if mode == ModeAuto {
		mode = ModePlain
		if f, ok := out.(*os.File); ok {
			if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
				mode = ModeTTY
			}
		}
	}
	return &Reporter{out: out, mode: mode}
}

// SetLabel names the task in JSON output.
func (r *Reporter) SetLabel(label string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.label = label
	r.mu.Unlock()
}

// Update draws or emits one progress sample.
func (r *Reporter) Update(p engine.Progress) {
	if r == nil || r.mode == ModeQuiet || r.out == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if r.started.IsZero() {
		r.started = now
		r.last = now
	}
	if elapsed := now.Sub(r.last).Seconds(); elapsed >= 0.2 {
		if p.ItemsTotal == 0 && p.Done >= r.lastDone {
			instant := float64(p.Done-r.lastDone) / elapsed
			if r.speed == 0 {
				r.speed = instant
			} else {
				r.speed = r.speed*0.7 + instant*0.3
			}
		}
		r.lastDone = p.Done
		r.last = now
	}
	elapsed := now.Sub(r.started)

	switch r.mode {
	case ModeTTY:
		r.renderTTY(p, elapsed)
	case ModePlain:
		if r.lastPrint.IsZero() || now.Sub(r.lastPrint) >= 2*time.Second {
			r.renderPlain(p)
			r.lastPrint = now
		}
	case ModeJSON:
		if r.lastPrint.IsZero() || now.Sub(r.lastPrint) >= time.Second {
			r.renderJSON(p, elapsed)
			r.lastPrint = now
		}
	}
}

// Done writes the terminal JSON event, if any, and clears a TTY line.
func (r *Reporter) Done(res *engine.Result, err error) {
	if r == nil || r.mode == ModeQuiet || r.out == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	elapsed := 0.0
	if !r.started.IsZero() {
		elapsed = time.Since(r.started).Seconds()
	}
	switch r.mode {
	case ModeTTY:
		fmt.Fprint(r.out, "\r\x1b[K")
	case ModeJSON:
		if err != nil {
			r.writeJSON(map[string]any{
				"event":   "error",
				"label":   r.label,
				"error":   err.Error(),
				"elapsed": elapsed,
			})
			return
		}
		if res != nil {
			r.writeJSON(map[string]any{
				"event":   "done",
				"label":   r.label,
				"path":    res.Path,
				"bytes":   res.Bytes,
				"files":   res.Files,
				"elapsed": elapsed,
			})
		}
	}
}

func (r *Reporter) renderTTY(p engine.Progress, elapsed time.Duration) {
	var b strings.Builder
	b.WriteString("\r\x1b[K")

	if p.ItemsTotal > 0 {
		frac := float64(p.ItemsDone) / float64(p.ItemsTotal)
		fmt.Fprintf(&b, " %5.1f%%  %s  %d/%d", frac*100, bar(frac, 20), p.ItemsDone, p.ItemsTotal)
		if p.Message != "" {
			b.WriteString("  ")
			b.WriteString(p.Message)
		}
		if p.Connections > 0 {
			fmt.Fprintf(&b, "  ×%d", p.Connections)
		}
		b.WriteString("  ")
		b.WriteString(clock(elapsed))
		fmt.Fprint(r.out, b.String())
		return
	}

	if p.Total > 0 {
		frac := math.Max(0, math.Min(1, float64(p.Done)/float64(p.Total)))
		fmt.Fprintf(&b, " %5.1f%%  %s  %s / %s",
			frac*100, bar(frac, 20), humanBytes(p.Done), humanBytes(p.Total))
		if r.speed > 0 {
			b.WriteString("  ")
			b.WriteString(rate(r.speed))
			if remaining := (p.Total - p.Done); remaining > 0 {
				b.WriteString("  ETA ")
				b.WriteString(clock(time.Duration(float64(remaining)/r.speed) * time.Second))
			}
		}
	} else {
		frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		r.spinner++
		fmt.Fprintf(&b, " %c  %s", frames[r.spinner%len(frames)], humanBytes(p.Done))
		if r.speed > 0 {
			b.WriteString("  ")
			b.WriteString(rate(r.speed))
		}
	}

	if p.Connections > 0 {
		fmt.Fprintf(&b, "  ×%d", p.Connections)
	}
	if p.Message != "" {
		b.WriteString("  ")
		b.WriteString(p.Message)
	}
	fmt.Fprint(r.out, b.String())
}

func (r *Reporter) renderPlain(p engine.Progress) {
	switch {
	case p.ItemsTotal > 0:
		fmt.Fprintf(r.out, "%d/%d %s\n", p.ItemsDone, p.ItemsTotal, p.Message)
	case p.Total > 0:
		fmt.Fprintf(r.out, "%s / %s (%.1f%%) %s\n",
			humanBytes(p.Done), humanBytes(p.Total),
			float64(p.Done)/float64(p.Total)*100, rate(r.speed))
	default:
		fmt.Fprintf(r.out, "%s %s\n", humanBytes(p.Done), rate(r.speed))
	}
}

func (r *Reporter) renderJSON(p engine.Progress, elapsed time.Duration) {
	r.writeJSON(map[string]any{
		"event":       "progress",
		"label":       r.label,
		"phase":       p.Phase,
		"done":        p.Done,
		"total":       p.Total,
		"itemsDone":   p.ItemsDone,
		"itemsTotal":  p.ItemsTotal,
		"connections": p.Connections,
		"speed":       math.Round(r.speed),
		"elapsed":     math.Round(elapsed.Seconds()*100) / 100,
		"message":     p.Message,
	})
}

func (r *Reporter) writeJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = r.out.Write(data)
}

func bar(frac float64, width int) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(math.Round(frac * float64(width)))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func humanBytes(n int64) string {
	if n < 0 {
		return "?"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func rate(bps float64) string {
	if bps <= 0 {
		return "—"
	}
	return humanBytes(int64(bps)) + "/s"
}

func clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int64(d / time.Second)
	return fmt.Sprintf("%02d:%02d", secs/60, secs%60)
}
