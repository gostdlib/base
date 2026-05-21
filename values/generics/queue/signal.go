package queue

import (
	"sync/atomic"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/context"
)

// signal is a Mesa-style broadcast primitive: Wait parks on the current generation
// channel; Signal swaps in a fresh channel and closes the old one to wake every
// parked Wait. There is no per-Wait goroutine, no per-Wait allocation, and no
// busy-wait in Signal — Signal is O(1) regardless of waiter count and Wait sits in
// the runtime's parked state until released.
type signal struct {
	mu      sync.Mutex
	ch      chan struct{} // current generation; closed to broadcast
	waiters atomic.Int32
}

// newSignal returns a signal in the "next Wait blocks" state.
func newSignal() *signal {
	return &signal{ch: make(chan struct{})}
}

// Wait parks until Signal is called or ctx is cancelled. unlock releases the
// caller's external lock; it is invoked AFTER Wait has synchronously registered
// as a waiter, closing the Mesa-style race where a Signal between "release
// external lock" and "enter Wait" would otherwise see waiters==0 and complete
// as a no-op. If the caller has no external lock to release, pass func(){}. If
// ctx is cancelled, context.Cause(ctx) is returned.
func (s *signal) Wait(ctx context.Context, unlock func()) error {
	s.mu.Lock()
	ch := s.ch
	s.waiters.Add(1)
	s.mu.Unlock()
	unlock()
	defer s.waiters.Add(-1)

	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-ch:
		return nil
	}
}

// HasWaiters reports whether at least one Wait is currently registered. Callers
// gate Signal on this so the steady-state (no parked waiter) case skips Signal
// entirely, keeping the hot path allocation-free.
func (s *signal) HasWaiters() bool { return s.waiters.Load() > 0 }

// Signal wakes every currently parked waiter and rearms the signal so the next
// Wait blocks. Signal does not wait for the woken Waits to return; they each
// observe the closed channel and return at their own pace. Concurrent Signals
// are safe: each captures its own old channel to close.
func (s *signal) Signal() {
	s.mu.Lock()
	old := s.ch
	s.ch = make(chan struct{})
	s.mu.Unlock()
	close(old)
}
