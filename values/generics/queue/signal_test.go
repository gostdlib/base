package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// waitForSignalWaiters polls s.waiters until it equals want or the deadline expires.
// It is needed because Wait() increments s.waiters synchronously but then submits a
// pool goroutine, and Signal() must be called only after all expected waiters have
// registered.
func waitForSignalWaiters(t *testing.T, s *signal, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.waiters.Load() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("waitForSignalWaiters: got waiters == %d, want %d", s.waiters.Load(), want)
}

// TestSignalWaitThenSignal verifies the basic flow: a goroutine blocked in Wait returns
// nil after Signal is called and the internal waiters counter is reset to zero.
func TestSignalWaitThenSignal(t *testing.T) {
	ctx := t.Context()
	s := newSignal(ctx, 4)

	done := make(chan error, 1)
	go func() { done <- s.Wait(ctx) }()

	waitForSignalWaiters(t, s, 1)
	s.Signal()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("TestSignalWaitThenSignal: got err == %s, want err == nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("TestSignalWaitThenSignal: Wait did not return within 2s after Signal")
	}
	if w := s.waiters.Load(); w != 0 {
		t.Errorf("TestSignalWaitThenSignal: got waiters == %d, want 0", w)
	}
}

// TestSignalMultipleWaiters verifies that a single Signal releases every registered
// waiter and the waiters counter returns to zero. This is the core broadcast guarantee
// of the signal type.
func TestSignalMultipleWaiters(t *testing.T) {
	tests := []struct {
		name string
		n    int32
	}{
		{name: "Success: two waiters", n: 2},
		{name: "Success: ten waiters", n: 10},
		{name: "Success: fifty waiters", n: 50},
	}

	for _, test := range tests {
		ctx := t.Context()
		s := newSignal(ctx, 8)

		done := make(chan error, test.n)
		for range test.n {
			go func() { done <- s.Wait(ctx) }()
		}
		waitForSignalWaiters(t, s, test.n)
		s.Signal()

		for i := int32(0); i < test.n; i++ {
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("TestSignalMultipleWaiters(%s): Wait #%d got err == %s, want err == nil", test.name, i, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("TestSignalMultipleWaiters(%s): only %d/%d Waits returned", test.name, i, test.n)
			}
		}
		if w := s.waiters.Load(); w != 0 {
			t.Errorf("TestSignalMultipleWaiters(%s): got waiters == %d, want 0", test.name, w)
		}
	}
}

// TestSignalContextCancel verifies that a Wait blocked on the signal returns
// context.Cause(ctx) when the per-call context is cancelled, before any Signal is
// issued. A final Signal is sent so the cleanup goroutine inside Wait drains its
// pooled channel and the waiters counter returns to zero, exercising the cancel-then-
// Signal teardown path.
func TestSignalContextCancel(t *testing.T) {
	parent := t.Context()
	s := newSignal(parent, 4)

	wantCause := errors.New("wait cancelled by test")
	ctx, cancel := context.WithCancelCause(parent)

	done := make(chan error, 1)
	go func() { done <- s.Wait(ctx) }()

	waitForSignalWaiters(t, s, 1)
	cancel(wantCause)

	select {
	case err := <-done:
		switch {
		case err == nil:
			t.Errorf("TestSignalContextCancel: got err == nil, want %s", wantCause)
		case !errors.Is(err, wantCause):
			t.Errorf("TestSignalContextCancel: got err == %s, want %s", err, wantCause)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("TestSignalContextCancel: Wait did not return within 2s after cancel")
	}

	// The cancelled Wait left a cleanup goroutine waiting for ch; Signal lets that
	// inner pool goroutine acquire RLock so the cleanup can drain ch and return the
	// channel to the pool. Signal itself busy-waits until waiters == 0, so once it
	// returns the counter must be zero.
	s.Signal()
	if w := s.waiters.Load(); w != 0 {
		t.Errorf("TestSignalContextCancel: after Signal got waiters == %d, want 0", w)
	}
}

// TestSignalParentContextCancel verifies that cancellation of the same context used to
// construct the signal also releases an in-flight Wait with the cause from the parent.
func TestSignalParentContextCancel(t *testing.T) {
	wantCause := errors.New("parent cancelled by test")
	ctx, cancel := context.WithCancelCause(t.Context())
	s := newSignal(ctx, 2)

	done := make(chan error, 1)
	go func() { done <- s.Wait(ctx) }()

	waitForSignalWaiters(t, s, 1)
	cancel(wantCause)

	select {
	case err := <-done:
		if !errors.Is(err, wantCause) {
			t.Errorf("TestSignalParentContextCancel: got err == %v, want %v", err, wantCause)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("TestSignalParentContextCancel: Wait did not return within 2s after cancel")
	}

	// Drain the cleanup goroutine so the signal is reusable / leak-free.
	s.Signal()
	if w := s.waiters.Load(); w != 0 {
		t.Errorf("TestSignalParentContextCancel: after Signal got waiters == %d, want 0", w)
	}
}

// TestSignalRepeatedCycles verifies that the signal can be reused across many
// Wait/Signal cycles. After each Signal the write lock is re-acquired, so subsequent
// Waits block again — without that, the second cycle's Wait would not register.
func TestSignalRepeatedCycles(t *testing.T) {
	ctx := t.Context()
	s := newSignal(ctx, 4)

	const cycles = 5
	for i := 0; i < cycles; i++ {
		done := make(chan error, 1)
		go func() { done <- s.Wait(ctx) }()

		waitForSignalWaiters(t, s, 1)
		s.Signal()

		select {
		case err := <-done:
			if err != nil {
				t.Errorf("TestSignalRepeatedCycles: cycle %d got err == %s, want err == nil", i, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("TestSignalRepeatedCycles: cycle %d Wait did not return within 2s after Signal", i)
		}
		if w := s.waiters.Load(); w != 0 {
			t.Errorf("TestSignalRepeatedCycles: cycle %d got waiters == %d, want 0", i, w)
		}
	}
}

// TestSignalNoWaiters verifies that Signal is safe to call when no Wait is pending. It
// must return promptly (the busy-wait loop sees waiters == 0 on first check) and leave
// the signal in the "next Wait blocks" state, which we confirm by registering a Wait
// after the no-op Signal and observing that it actually blocks until a second Signal.
func TestSignalNoWaiters(t *testing.T) {
	ctx := t.Context()
	s := newSignal(ctx, 2)

	signalReturned := make(chan struct{})
	go func() {
		s.Signal()
		close(signalReturned)
	}()
	select {
	case <-signalReturned:
	case <-time.After(2 * time.Second):
		t.Fatalf("TestSignalNoWaiters: Signal with no waiters did not return within 2s")
	}

	// After Signal with no waiters, the write lock should be held again so a new
	// Wait blocks. Verify by polling done.
	done := make(chan error, 1)
	go func() { done <- s.Wait(ctx) }()

	waitForSignalWaiters(t, s, 1)
	select {
	case err := <-done:
		t.Fatalf("TestSignalNoWaiters: Wait returned (err=%v) before second Signal; signal is not in blocking state", err)
	case <-time.After(50 * time.Millisecond):
		// Expected: Wait still blocked.
	}

	s.Signal()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("TestSignalNoWaiters: post-Signal Wait got err == %s, want err == nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("TestSignalNoWaiters: Wait did not return within 2s after second Signal")
	}
}

// TestSignalWaitBlocksUntilSignal verifies that Wait actually blocks: a Wait launched
// on a fresh signal must not return before Signal is called. We assert non-return for
// a short window, then verify Signal releases it.
func TestSignalWaitBlocksUntilSignal(t *testing.T) {
	ctx := t.Context()
	s := newSignal(ctx, 2)

	done := make(chan error, 1)
	go func() { done <- s.Wait(ctx) }()

	waitForSignalWaiters(t, s, 1)

	select {
	case err := <-done:
		t.Fatalf("TestSignalWaitBlocksUntilSignal: Wait returned (err=%v) without Signal", err)
	case <-time.After(50 * time.Millisecond):
		// Expected: Wait is blocked.
	}

	s.Signal()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("TestSignalWaitBlocksUntilSignal: got err == %s, want err == nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("TestSignalWaitBlocksUntilSignal: Wait did not return within 2s after Signal")
	}
}

// TestSignalMixedCancelAndSuccess verifies a tricky concurrent scenario: among N
// waiters, half have their per-call context cancelled before Signal, the other half
// receive a successful Signal. The cancelled waiters must return the cancel cause and
// the successful ones must return nil. After Signal completes, the waiters counter
// must be zero — meaning the cleanup goroutines for cancelled Waits drained their
// channels.
func TestSignalMixedCancelAndSuccess(t *testing.T) {
	parent := t.Context()
	s := newSignal(parent, 8)

	const n = 10
	wantCause := errors.New("mixed cancel")
	type result struct {
		idx int
		err error
	}
	results := make(chan result, n)
	cancels := make([]context.CancelCauseFunc, n)

	for i := range n {
		ctx, cancel := context.WithCancelCause(parent)
		cancels[i] = cancel
		go func() { results <- result{idx: i, err: s.Wait(ctx)} }()
	}
	waitForSignalWaiters(t, s, n)

	// Cancel the even-indexed Waits.
	for i := 0; i < n; i += 2 {
		cancels[i](wantCause)
	}

	// Collect cancellations first so we know they've returned.
	cancelled := 0
	for cancelled < n/2 {
		select {
		case r := <-results:
			if r.idx%2 != 0 {
				t.Fatalf("TestSignalMixedCancelAndSuccess: unexpected early return for idx %d (err=%v)", r.idx, r.err)
			}
			if !errors.Is(r.err, wantCause) {
				t.Errorf("TestSignalMixedCancelAndSuccess: cancelled Wait #%d got err == %v, want %v", r.idx, r.err, wantCause)
			}
			cancelled++
		case <-time.After(2 * time.Second):
			t.Fatalf("TestSignalMixedCancelAndSuccess: only %d/%d cancelled Waits returned", cancelled, n/2)
		}
	}

	// Now Signal the remaining odd-indexed Waits (and also lets the cancelled Waits'
	// cleanup goroutines drain).
	s.Signal()

	for i := 0; i < n/2; i++ {
		select {
		case r := <-results:
			if r.idx%2 != 1 {
				t.Errorf("TestSignalMixedCancelAndSuccess: post-Signal return for even idx %d (err=%v)", r.idx, r.err)
			}
			if r.err != nil {
				t.Errorf("TestSignalMixedCancelAndSuccess: Wait #%d got err == %s, want err == nil", r.idx, r.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("TestSignalMixedCancelAndSuccess: only %d/%d successful Waits returned", i, n/2)
		}
	}

	// Clean up the leftover cancel funcs.
	for _, c := range cancels {
		c(nil)
	}

	if w := s.waiters.Load(); w != 0 {
		t.Errorf("TestSignalMixedCancelAndSuccess: got waiters == %d, want 0", w)
	}
}

// TestSignalAlreadyCancelledCtx verifies the edge case where ctx is already cancelled
// before Wait is called. Wait must still register as a waiter, return the cause, and
// the cleanup path must eventually decrement the counter once Signal fires.
func TestSignalAlreadyCancelledCtx(t *testing.T) {
	parent := t.Context()
	s := newSignal(parent, 2)

	wantCause := errors.New("pre-cancelled")
	ctx, cancel := context.WithCancelCause(parent)
	cancel(wantCause)

	done := make(chan error, 1)
	go func() { done <- s.Wait(ctx) }()

	select {
	case err := <-done:
		if !errors.Is(err, wantCause) {
			t.Errorf("TestSignalAlreadyCancelledCtx: got err == %v, want %v", err, wantCause)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("TestSignalAlreadyCancelledCtx: Wait did not return within 2s on pre-cancelled ctx")
	}

	s.Signal()
	if w := s.waiters.Load(); w != 0 {
		t.Errorf("TestSignalAlreadyCancelledCtx: after Signal got waiters == %d, want 0", w)
	}
}

// TestSignalPoolSizeZero verifies that newSignal works with poolSize == 0; the
// sync.Pool then falls back to its New() func for every channel and never buffers.
// Waits must still block, Signal must still release them.
func TestSignalPoolSizeZero(t *testing.T) {
	ctx := t.Context()
	s := newSignal(ctx, 0)

	const n = 4
	done := make(chan error, n)
	for range n {
		go func() { done <- s.Wait(ctx) }()
	}
	waitForSignalWaiters(t, s, n)
	s.Signal()

	for i := 0; i < n; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("TestSignalPoolSizeZero: Wait #%d got err == %s, want err == nil", i, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("TestSignalPoolSizeZero: only %d/%d Waits returned", i, n)
		}
	}
	if w := s.waiters.Load(); w != 0 {
		t.Errorf("TestSignalPoolSizeZero: got waiters == %d, want 0", w)
	}
}

// TestSignalConcurrentSignals exercises a sequence of overlapping Wait/Signal pairs
// to surface races between Signal's busy-wait loop and Wait's waiter-counter
// increments. Each iteration registers a fresh batch of waiters and signals them,
// asserting that the counter always lands back at zero before the next batch.
func TestSignalConcurrentSignals(t *testing.T) {
	ctx := t.Context()
	s := newSignal(ctx, 8)

	const cycles = 20
	const perCycle = 8
	for c := 0; c < cycles; c++ {
		var received atomic.Int32
		done := make(chan struct{}, perCycle)
		for range perCycle {
			go func() {
				if err := s.Wait(ctx); err == nil {
					received.Add(1)
				}
				done <- struct{}{}
			}()
		}
		waitForSignalWaiters(t, s, perCycle)
		s.Signal()

		for i := 0; i < perCycle; i++ {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("TestSignalConcurrentSignals: cycle %d only %d/%d Waits returned", c, i, perCycle)
			}
		}
		if got := received.Load(); got != perCycle {
			t.Errorf("TestSignalConcurrentSignals: cycle %d got %d successful Waits, want %d", c, got, perCycle)
		}
		if w := s.waiters.Load(); w != 0 {
			t.Errorf("TestSignalConcurrentSignals: cycle %d got waiters == %d, want 0", c, w)
		}
	}
}
