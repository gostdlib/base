package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimited(t *testing.T) {
	t.Parallel()

	exit := make(chan struct{}, 1)
	var done atomic.Int32
	f := func() {
		<-exit
		done.Add(1)
	}

	ctx := context.Background()
	p, err := New(ctx, "")
	if err != nil {
		panic(err)
	}

	l := p.Limited(4)

	l.Submit(ctx, f)
	l.Submit(ctx, f)
	l.Submit(ctx, f)
	l.Submit(ctx, f)

	blockedHappened := make(chan struct{})
	go func() {
		defer close(blockedHappened)
	}()

	if done.Load() != 0 {
		t.Fatalf("TestLimited: a function ended early")
	}

	select {
	case <-blockedHappened:
		t.Fatalf("TestLimited: a function that should have been blocked wasn't")
	default:
	}
	exit <- struct{}{}
	time.Sleep(100 * time.Millisecond)
	select {
	case <-blockedHappened:
	default:
		t.Fatalf("TestLimited: a blocked function should have completed")
	}

	if done.Load() != 1 {
		t.Fatalf("TestLimited: goroutine should have finished")
	}

	for i := 0; i < 3; i++ {
		exit <- struct{}{}
	}

	time.Sleep(100 * time.Millisecond)

	if done.Load() != 4 {
		t.Fatalf("TestLimited: all goroutines should have finished")
	}
}
