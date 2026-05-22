package queue

import (
	"sync/atomic"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/values/generics/queue/internal/parker"
)

// signal is a Mesa-style broadcast primitive. Wait registers a parker on
// the current generation and parks via runtime.gopark; Signal calls
// Broadcast on the parker waiter to wake every parked Wait. There is no
// per-Wait channel allocation and no busy-wait — Signal is O(parkers) and
// each Wait sits in the runtime's parked state until released.
type signal struct {
	mu      sync.Mutex
	waiter  *parker.Waiter
	waiters atomic.Int32
}

// newSignal returns a signal in the "next Wait blocks" state.
func newSignal() *signal {
	return &signal{waiter: parker.New()}
}

// Wait parks until Signal is called or ctx is cancelled. unlock releases
// the caller's external lock; it is invoked AFTER Wait has synchronously
// registered as a waiter, closing the Mesa-style race where a Signal
// between "release external lock" and "enter Wait" would otherwise see
// waiters==0 and complete as a no-op. If the caller has no external lock
// to release, pass func(){}. If ctx is cancelled, context.Cause(ctx) is
// returned.
func (s *signal) Wait(ctx context.Context, unlock func()) error {
	s.mu.Lock()
	p := s.waiter.Register()
	s.waiters.Add(1)
	s.mu.Unlock()
	unlock()
	defer s.waiters.Add(-1)

	// For non-cancellable ctx (e.g. context.Background) skip ctx watching
	// entirely and recycle the parker on the way out.
	if ctx.Done() == nil {
		p.Park()
		p.Release()
		return nil
	}

	// context.AfterFunc registers p.Wake to fire if ctx is cancelled. The
	// callback's goroutine is spawned lazily — only if ctx actually fires.
	// In the common case where Broadcast wakes us first, cancel() returns
	// true (the callback never ran) and we recycle p.
	// WakeFunc returns the pre-bound p.Wake — avoids the per-call
	// bound-method-value allocation that `ctx.AfterFunc(ctx, p.Wake)`
	// would otherwise incur.
	cancel := context.AfterFunc(ctx, p.WakeFunc())
	p.Park()
	if cancel() {
		// AfterFunc callback never ran: nothing else holds a reference
		// to p, safe to recycle through the pool.
		p.Release()
	} else {
		// Callback either ran or is running. Pool recycling would race
		// with Wake, but the callback never touches p.waiter, so we can
		// still detach p from the Waiter's list. Without this, every
		// ctx-cancelled Wait would leave a stale parker in the list,
		// growing it unboundedly under repeated timed-out waits and
		// making a later Signal() O(stale). The parker is then GC'd
		// once the AfterFunc registration releases its ref.
		p.Detach()
	}

	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	return nil
}

// HasWaiters reports whether at least one Wait is currently registered.
// Callers gate Signal on this so the steady-state (no parked waiter) case
// skips Signal entirely, keeping the hot path allocation-free.
func (s *signal) HasWaiters() bool { return s.waiters.Load() > 0 }

// Signal wakes every currently parked waiter and rearms the signal so the
// next Wait blocks. Signal does not wait for the woken Waits to return;
// each parker's gopark returns at its own pace. Concurrent Signals are
// safe: parker.Broadcast serializes through its own mutex.
func (s *signal) Signal() {
	s.waiter.Broadcast()
}
