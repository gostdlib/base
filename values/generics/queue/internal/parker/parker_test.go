package parker

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// Run all tests in this file with -race. The only non-atomic data flow
// in parker.go is the write to parker.g inside unlockf and the read of
// parker.g inside broadcast/reset. That pair is synchronized through
// parker.state (CAS to Parked → Swap from Parked); if the analysis is
// wrong, the race detector flags it here under load.

// waitForParkers spins until at least n parkers are registered on w.
// Without it, a broadcast that wins the race before any wait() reaches
// the register step proves nothing about the parked-and-woken path.
func waitForParkers(t *testing.T, w *waiter, n int) {
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

func TestWaitBroadcastBasic(t *testing.T) {
	w := newWaiter()
	done := make(chan struct{})
	go func() {
		w.wait()
		close(done)
	}()
	waitForParkers(t, w, 1)
	w.broadcast()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("TestWaitBroadcastBasic: wait did not return after broadcast")
	}
}

func TestBroadcastBeforeWait(t *testing.T) {
	w := newWaiter()
	w.broadcast()
	done := make(chan struct{})
	go func() {
		w.wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("TestBroadcastBeforeWait: wait did not return after pre-broadcast")
	}
}

func TestManyWaitersOneBroadcast(t *testing.T) {
	const n = 64
	w := newWaiter()
	woken := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			w.wait()
			woken <- struct{}{}
		}()
	}
	waitForParkers(t, w, n)
	w.broadcast()
	for i := 0; i < n; i++ {
		select {
		case <-woken:
		case <-time.After(2 * time.Second):
			t.Fatalf("TestManyWaitersOneBroadcast: only %d/%d waiters returned", i, n)
		}
	}
}

func TestResetCycles(t *testing.T) {
	const (
		cycles  = 200
		waiters = 8
	)
	w := newWaiter()
	for c := 0; c < cycles; c++ {
		done := make(chan struct{}, waiters)
		for i := 0; i < waiters; i++ {
			go func() {
				w.wait()
				done <- struct{}{}
			}()
		}
		waitForParkers(t, w, waiters)
		w.broadcast()
		for i := 0; i < waiters; i++ {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("TestResetCycles: cycle %d only saw %d/%d waiters return", c, i, waiters)
			}
		}
		w.reset()
	}
}

// TestRaceUnsynchronized: hammer wait+broadcast with no rendezvous so
// every interleaving (broadcast-before-register, broadcast-during-park,
// broadcast-after-park) gets exercised. This is the workhorse case for
// -race: parker.g is the only non-atomic data passed between goroutines.
func TestRaceUnsynchronized(t *testing.T) {
	if testing.Short() {
		t.Skip("TestRaceUnsynchronized: skipping race stress in -short")
	}
	const iters = 5000
	for i := 0; i < iters; i++ {
		w := newWaiter()
		var woken atomic.Bool
		done := make(chan struct{})
		go func() {
			w.wait()
			woken.Store(true)
			close(done)
		}()
		w.broadcast()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("TestRaceUnsynchronized: iter %d: wait did not return", i)
		}
		if !woken.Load() {
			t.Fatalf("TestRaceUnsynchronized: iter %d: waiter did not record wake", i)
		}
	}
}

// TestRaceConcurrentBroadcasts: many goroutines call broadcast() at once.
// One bumps gen even→odd; the rest must see odd and bail idempotently
// without double-waking the single parked waiter.
func TestRaceConcurrentBroadcasts(t *testing.T) {
	const (
		iters        = 500
		broadcasters = 16
	)
	for i := 0; i < iters; i++ {
		w := newWaiter()
		done := make(chan struct{})
		go func() {
			w.wait()
			close(done)
		}()
		waitForParkers(t, w, 1)
		start := make(chan struct{})
		bdone := make(chan struct{}, broadcasters)
		for b := 0; b < broadcasters; b++ {
			go func() {
				<-start
				w.broadcast()
				bdone <- struct{}{}
			}()
		}
		close(start)
		for b := 0; b < broadcasters; b++ {
			<-bdone
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("TestRaceConcurrentBroadcasts: iter %d: wait did not return", i)
		}
	}
}

// TestRaceBroadcastReset: drive many broadcast+reset cycles with no
// waitForParkers rendezvous. Waiters that don't reach the register step
// before broadcast must return spuriously via the gen mismatch path; the
// test still requires every spawned waiter to complete each cycle.
func TestRaceBroadcastReset(t *testing.T) {
	if testing.Short() {
		t.Skip("TestRaceBroadcastReset: skipping race stress in -short")
	}
	const (
		cycles  = 1000
		waiters = 4
	)
	w := newWaiter()
	for c := 0; c < cycles; c++ {
		done := make(chan struct{}, waiters)
		for i := 0; i < waiters; i++ {
			go func() {
				w.wait()
				done <- struct{}{}
			}()
		}
		w.broadcast()
		for i := 0; i < waiters; i++ {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("TestRaceBroadcastReset: cycle %d: %d/%d waiters returned", c, i, waiters)
			}
		}
		w.reset()
	}
}
