package engine

import (
	"context"
	"testing"
	"time"
)

func TestLimiterNil(t *testing.T) {
	if l := NewLimiter(0); l != nil {
		t.Fatalf("NewLimiter(0) = %v, want nil", l)
	}
	var l *Limiter
	if err := l.WaitN(context.Background(), 1024); err != nil {
		t.Fatalf("nil limiter: %v", err)
	}
}

func TestLimiterCtxCancelled(t *testing.T) {
	l := NewLimiter(1 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.WaitN(ctx, 1); err == nil {
		t.Fatal("want cancellation error")
	}
}

func TestLimiterThrottles(t *testing.T) {
	l := NewLimiter(1024) // 1 KiB/s, burst 64 KiB
	if err := l.WaitN(context.Background(), 64*1024); err != nil {
		t.Fatalf("initial burst: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := l.WaitN(ctx, 64*1024)
	if err == nil {
		t.Fatal("want throttling wait to hit the context deadline")
	}
	if time.Since(start) < 10*time.Millisecond {
		t.Fatalf("returned too fast: %v", time.Since(start))
	}
}
