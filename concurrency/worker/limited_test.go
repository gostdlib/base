package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegressionLimitedCancelRelease(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p, err := New(ctx, "")
	if err != nil {
		panic(err)
	}

	const limit = 2
	l := p.Limited(t.Context(), "", limit)

	// Submit with an already-cancelled context. The token must be released
	// even though the job never runs, otherwise the pool leaks capacity.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	for i := 0; i < limit; i++ {
		l.Submit(cancelled, func() { t.Errorf("TestRegressionLimitedCancelRelease: job should not run on cancelled context") })
	}

	// If tokens leaked, this submit on a live context will deadlock.
	done := make(chan struct{})
	go func() {
		l.Submit(ctx, func() {})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("TestRegressionLimitedCancelRelease: submit blocked, token was not released on cancelled context")
	}
}

func TestLimited(t *testing.T) {
	t.Parallel()

	exit := make(chan struct{}, 1)
	var done atomic.Int32
	startedWG := sync.WaitGroup{}
	startedWG.Add(4)
	f := func() {
		startedWG.Done()
		<-exit
		done.Add(1)
	}

	ctx := context.Background()
	p, err := New(ctx, "")
	if err != nil {
		panic(err)
	}

	l := p.Limited(t.Context(), "", 4)
	l.Submit(ctx, f)
	l.Submit(ctx, f)
	l.Submit(ctx, f)
	l.Submit(ctx, f)

	// Wait for each of these to start running.
	startedWG.Wait()

	blockedHappened := make(chan struct{})
	go func() {
		l.Submit(
			ctx,
			func() {
				defer close(blockedHappened)
			},
		)
	}()

	// Make sure this is working like we think.
	if done.Load() != 0 {
		t.Fatalf("TestLimited: a function ended early")
	}

	// Give the blockHappened function time to attempt to run (it shouldn't due to limited jobs).
	time.Sleep(100 * time.Millisecond)

	// Test to make sure that didn't happen.
	select {
	case <-blockedHappened:
		t.Fatalf("TestLimited: a function that should have been blocked wasn't")
	default:
	}

	// Cause all the 1 of the waiting goroutines to run, freeing up blockHappened to be closed.
	exit <- struct{}{}

	// Wait for blockHappened to close.
	time.Sleep(100 * time.Millisecond)
	select {
	case <-blockedHappened:
	default:
		t.Fatalf("TestLimited: a blocked function should have completed")
	}

	// One of our original goroutines should be done.
	if done.Load() != 1 {
		t.Fatalf("TestLimited: goroutine should have finished")
	}

	// Force the others to finish.
	close(exit)

	// Give time for each to finish.
	time.Sleep(100 * time.Millisecond)

	if done.Load() != 4 {
		t.Fatalf("TestLimited: all goroutines should have finished")
	}
}
