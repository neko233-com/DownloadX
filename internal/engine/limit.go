package engine

import (
	"context"
	"math"
	"sync"
	"time"
)

// Limiter is a token bucket shared by all workers of one task.
// A nil *Limiter means unlimited and is safe to call.
type Limiter struct {
	mu     sync.Mutex
	rate   float64 // bytes per second
	burst  float64
	tokens float64
	last   time.Time
}

// NewLimiter returns a limiter capped at bytesPerSec, or nil when the rate is
// zero or negative (unlimited).
func NewLimiter(bytesPerSec int64) *Limiter {
	if bytesPerSec <= 0 {
		return nil
	}
	burst := math.Max(64*1024, float64(bytesPerSec))
	return &Limiter{
		rate:   float64(bytesPerSec),
		burst:  burst,
		tokens: burst,
		last:   time.Now(),
	}
}

// WaitN blocks until n bytes may be transferred or ctx is cancelled.
func (l *Limiter) WaitN(ctx context.Context, n int) error {
	if l == nil || n <= 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	for {
		l.mu.Lock()
		now := time.Now()
		l.tokens = math.Min(l.burst, l.tokens+now.Sub(l.last).Seconds()*l.rate)
		l.last = now

		need := float64(n)
		if need > l.burst {
			need = l.burst
		}
		if l.tokens >= need {
			l.tokens -= need
			l.mu.Unlock()
			return nil
		}
		wait := time.Duration((need - l.tokens) / l.rate * float64(time.Second))
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		l.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
