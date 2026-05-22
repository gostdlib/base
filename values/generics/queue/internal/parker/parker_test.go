package parker

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// Run all tests with -race. The only non-atomic data flow is the write to
// Parker.g inside unlockf and the read in Broadcast/Wake. That pair is
// synchronized through Parker.state (CAS to Parked → Swap from Parked); if
// the analysis is wrong, the race detector flags it under load.

// waitForParkers spins until at least n parkers are registered on w.
func waitForParkers(t *testing.T, w *Waiter, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		w.mu.Lock()
		have := len(w.parkers)
		w.mu.Unlock()
		if have >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: only %d/%d parkers registered after 2s", t.Name(), have, n)
		}
		runtime.Gosched()
	}
}

func TestRegisterParkBroadcast(t *testing.T) {
	w := New()
	done := make(chan struct{})
	go func() {
		p := w.Register()
		p.Park()
		close(done)
	}()
	waitForParkers(t, w, 1)
	w.Broadcast()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("TestRegisterParkBroadcast: Park did not return after Broadcast")
	}
}

func TestManyWaitersOneBroadcast(t *testing.T) {
	const n = 64
	w := New()
	woken := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			p := w.Register()
			p.Park()
			woken <- struct{}{}
		}()
	}
	waitForParkers(t, w, n)
	w.Broadcast()
	for i := 0; i < n; i++ {
		select {
		case <-woken:
		case <-time.After(2 * time.Second):
			t.Fatalf("TestManyWaitersOneBroadcast: only %d/%d waiters returned", i, n)
		}
	}
}

func TestWakeBeforePark(t *testing.T) {
	w := New()
	p := w.Register()
	p.Wake()
	done := make(chan struct{})
	go func() {
		p.Park()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("TestWakeBeforePark: Park did not return after pre-Wake")
	}
}

func TestWakeAfterPark(t *testing.T) {
	w := New()
	done := make(chan struct{})
	go func() {
		p := w.Register()
		p.Park()
		close(done)
	}()
	waitForParkers(t, w, 1)
	// Reach into the waiter to grab the parker we just registered, then
	// wake only that one (mimics signal.Wait's ctx-cancellation path).
	w.mu.Lock()
	p := w.parkers[0]
	w.mu.Unlock()
	p.Wake()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("TestWakeAfterPark: Park did not return after Wake")
	}
}

func TestBroadcastIsIdempotent(t *testing.T) {
	w := New()
	done := make(chan struct{})
	go func() {
		p := w.Register()
		p.Park()
		close(done)
	}()
	waitForParkers(t, w, 1)
	w.Broadcast()
	w.Broadcast() // no-op: list already drained
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("TestBroadcastIsIdempotent: Park did not return")
	}
}

// TestRaceRegisterParkBroadcast exercises the narrow race window between
// Register (parker visible in the list) and Park (CAS state to parkParked
// inside unlockf): Broadcast may run before or after the CAS, and both
// orderings must lead to Park returning. Workhorse case for -race —
// Parker.g is the only non-atomic data passed between goroutines.
//
// The caller-side rendezvous (registered channel) matches the Mesa-style
// contract: Broadcast may not race ahead of Register or the wake is lost.
// The race we want -race to police is purely between unlockf's CAS and
// Broadcast's Swap on the same parker.state.
func TestRaceRegisterParkBroadcast(t *testing.T) {
	if testing.Short() {
		t.Skip("TestRaceRegisterParkBroadcast: skipping race stress in -short")
	}
	const iters = 5000
	for i := 0; i < iters; i++ {
		w := New()
		var woken atomic.Bool
		registered := make(chan struct{})
		done := make(chan struct{})
		go func() {
			p := w.Register()
			close(registered)
			p.Park()
			woken.Store(true)
			close(done)
		}()
		<-registered
		w.Broadcast()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("TestRaceRegisterParkBroadcast: iter %d: Park did not return", i)
		}
		if !woken.Load() {
			t.Fatalf("TestRaceRegisterParkBroadcast: iter %d: waiter did not record wake", i)
		}
	}
}

// TestRaceWakeVsBroadcast: each iteration races a per-parker Wake against
// a Broadcast targeting the same parker. Both must be safe; the waiter
// must wake exactly once.
func TestRaceWakeVsBroadcast(t *testing.T) {
	if testing.Short() {
		t.Skip("TestRaceWakeVsBroadcast: skipping race stress in -short")
	}
	const iters = 5000
	for i := 0; i < iters; i++ {
		w := New()
		done := make(chan struct{})
		var p *Parker
		ready := make(chan struct{})
		go func() {
			p = w.Register()
			close(ready)
			p.Park()
			close(done)
		}()
		<-ready
		go w.Broadcast()
		go p.Wake()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("TestRaceWakeVsBroadcast: iter %d: Park did not return", i)
		}
	}
}

// TestRaceManyCycles: many Broadcast cycles with several concurrent
// waiters per cycle, no synchronization between cycles. Exercises the
// parker list churn under load.
func TestRaceManyCycles(t *testing.T) {
	if testing.Short() {
		t.Skip("TestRaceManyCycles: skipping race stress in -short")
	}
	const (
		cycles  = 500
		waiters = 4
	)
	w := New()
	for c := 0; c < cycles; c++ {
		done := make(chan struct{}, waiters)
		for i := 0; i < waiters; i++ {
			go func() {
				p := w.Register()
				p.Park()
				done <- struct{}{}
			}()
		}
		waitForParkers(t, w, waiters)
		w.Broadcast()
		for i := 0; i < waiters; i++ {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("TestRaceManyCycles: cycle %d: %d/%d waiters returned", c, i, waiters)
			}
		}
	}
}
