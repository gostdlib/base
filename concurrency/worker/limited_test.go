package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
