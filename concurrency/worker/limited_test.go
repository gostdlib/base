package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

func TestRegressionLimitedCancelRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		p, err := New(ctx, "")
		if err != nil {
			panic(err)
		}
		defer p.Close(ctx)

		const limit = 2
		l := p.Limited(ctx, "", limit)

		// Submit with an already-cancelled context. The token must be released
		// even though the job never runs, otherwise the pool leaks capacity.
		cancelled, cancel := context.WithCancel(ctx)
		cancel()

		for i := 0; i < limit; i++ {
			if l.Submit(cancelled, func() { t.Errorf("TestRegressionLimitedCancelRelease: job should not run on cancelled context") }) {
				t.Errorf("TestRegressionLimitedCancelRelease: Submit on a cancelled context returned true, want false")
			}
		}

		// If tokens leaked, this submit on a live context would block forever and the bubble's
		// deadlock detector would fail the test.
		done := make(chan struct{})
		go func() {
			if !l.Submit(ctx, func() {}) {
				t.Errorf("TestRegressionLimitedCancelRelease: Submit on a live context returned false, want true")
			}
			close(done)
		}()

		<-done
	})
}

func TestLimit(t *testing.T) {
	ctx := context.Background()
	root, err := New(ctx, "")
	if err != nil {
		panic(err)
	}
	defer root.Close(ctx)

	tests := []struct {
		name string
		pool *Pool
		want int
	}{
		{
			name: "Success: a root pool is unlimited",
			pool: root,
			want: 0,
		},
		{
			name: "Success: Sub of an unlimited pool is unlimited",
			pool: root.Sub(ctx, ""),
			want: 0,
		},
		{
			name: "Success: Limited sets the limit",
			pool: root.Limited(ctx, "", 5),
			want: 5,
		},
		{
			name: "Success: Sub of a Limited pool inherits the parent limit",
			pool: root.Limited(ctx, "", 5).Sub(ctx, ""),
			want: 5,
		},
		{
			name: "Success: Limited smaller than the parent uses the smaller limit",
			pool: root.Limited(ctx, "", 5).Limited(ctx, "", 3),
			want: 3,
		},
		{
			name: "Success: Limited larger than the parent is capped to the parent limit",
			pool: root.Limited(ctx, "", 5).Limited(ctx, "", 10),
			want: 5,
		},
	}

	for _, test := range tests {
		if got := test.pool.Limit(); got != test.want {
			t.Errorf("TestLimit(%s): got %d, want %d", test.name, got, test.want)
		}
	}
}

// TestRegressionNestedLimit verifies that a Sub() or Limited() pool derived from a Limited parent is still
// bounded by the parent's limit. Previously the child dropped the parent's limit and could run unbounded.
func TestRegressionNestedLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()

		tests := []struct {
			name   string
			derive func(root *Pool) *Pool
			want   int
		}{
			{
				name:   "Success: Sub of a Limited pool is bounded by the parent limit",
				derive: func(root *Pool) *Pool { return root.Limited(ctx, "", 2).Sub(ctx, "") },
				want:   2,
			},
			{
				name:   "Success: Limited larger than the parent is capped to the parent limit",
				derive: func(root *Pool) *Pool { return root.Limited(ctx, "", 2).Limited(ctx, "", 10) },
				want:   2,
			},
			{
				name:   "Success: Limited smaller than the parent uses the smaller limit",
				derive: func(root *Pool) *Pool { return root.Limited(ctx, "", 3).Limited(ctx, "", 1) },
				want:   1,
			},
		}

		for _, test := range tests {
			root, err := New(ctx, "")
			if err != nil {
				panic(err)
			}
			assertBound(t, ctx, test.name, test.derive(root), test.want)
			root.Close(ctx)
		}
	})
}

// TestRegressionNestedLimitSiblingStarvation verifies that a submit blocked on a narrow child limit does not
// hold a broad parent token and starve a sibling pool. With parent limit 2 and two children each limited to 1,
// a blocked second submit to child A must not prevent child B from running while only one job is active.
func TestRegressionNestedLimitSiblingStarvation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		root, err := New(ctx, "")
		if err != nil {
			panic(err)
		}
		defer root.Close(ctx)

		parent := root.Limited(ctx, "", 2)
		a := parent.Limited(ctx, "", 1)
		b := parent.Limited(ctx, "", 1)

		exit := make(chan struct{})
		var running atomic.Int32
		block := func() {
			running.Add(1)
			<-exit
			running.Add(-1)
		}

		// A job runs, holding A's only slot and one of the parent's two slots.
		aStarted := make(chan struct{})
		if !a.Submit(ctx, func() { close(aStarted); block() }) {
			t.Fatalf("TestRegressionNestedLimitSiblingStarvation: A job Submit returned false, want true")
		}
		<-aStarted

		// A second A submit cannot run (A is full). It must block on A's semaphore without grabbing a parent
		// token, otherwise it starves B.
		go func() {
			if !a.Submit(ctx, block) {
				t.Errorf("TestRegressionNestedLimitSiblingStarvation: second A Submit returned false, want true")
			}
		}()

		// B must be able to run: only one job is actually running, so a parent slot is free.
		bRan := make(chan struct{})
		go func() {
			if !b.Submit(ctx, func() { close(bRan); block() }) {
				t.Errorf("TestRegressionNestedLimitSiblingStarvation: B Submit returned false, want true")
			}
		}()

		synctest.Wait()
		select {
		case <-bRan:
		default:
			t.Fatalf("TestRegressionNestedLimitSiblingStarvation: B was starved by a blocked A submit holding a parent token")
		}
		if got := running.Load(); got != 2 {
			t.Fatalf("TestRegressionNestedLimitSiblingStarvation: got %d jobs running, want 2 (A job + B job)", got)
		}

		// Release everything so the pool drains, including the blocked second A submit.
		close(exit)
		synctest.Wait()
	})
}

// assertBound verifies that pool l runs at most want jobs concurrently: it saturates l with want blocking
// jobs, confirms exactly want run while a (want+1)th submit is durably blocked, then releases them and
// confirms the extra job runs. Must be called inside a synctest bubble.
func assertBound(t *testing.T, ctx context.Context, name string, l *Pool, want int) {
	t.Helper()

	exit := make(chan struct{})
	var running atomic.Int32
	startedWG := sync.WaitGroup{}
	startedWG.Add(want)
	f := func() {
		running.Add(1)
		startedWG.Done()
		<-exit
		running.Add(-1)
	}

	// Fill every slot. These submits acquire immediately and must succeed.
	for i := 0; i < want; i++ {
		if !l.Submit(ctx, f) {
			t.Fatalf("TestRegressionNestedLimit(%s): Submit returned false, want true", name)
		}
	}
	startedWG.Wait()

	// One more than the limit: this submit must block acquiring a slot, so its job must not run.
	extraRan := make(chan struct{})
	go func() {
		if !l.Submit(ctx, func() { close(extraRan) }) {
			t.Errorf("TestRegressionNestedLimit(%s): extra Submit returned false, want true", name)
		}
	}()

	synctest.Wait()
	if got := int(running.Load()); got != want {
		t.Fatalf("TestRegressionNestedLimit(%s): got %d jobs running concurrently, want %d", name, got, want)
	}
	select {
	case <-extraRan:
		t.Fatalf("TestRegressionNestedLimit(%s): a job ran beyond the concurrency limit of %d", name, want)
	default:
	}

	// Release everything so the extra job can run and the pool drains.
	close(exit)
	synctest.Wait()
	select {
	case <-extraRan:
	default:
		t.Fatalf("TestRegressionNestedLimit(%s): the extra job never ran after slots freed", name)
	}
}

func TestLimited(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
		defer p.Close(ctx)

		l := p.Limited(ctx, "", 4)
		for i := 0; i < 4; i++ {
			if !l.Submit(ctx, f) {
				t.Fatalf("TestLimited: Submit on a live context returned false, want true")
			}
		}

		// Wait for each of these to start running.
		startedWG.Wait()

		blockedHappened := make(chan struct{})
		go func() {
			// This submit blocks until a slot frees, then runs, so it must report true.
			if !l.Submit(ctx, func() { defer close(blockedHappened) }) {
				t.Errorf("TestLimited: blocked Submit on a live context returned false, want true")
			}
		}()

		// Make sure this is working like we think.
		if done.Load() != 0 {
			t.Fatalf("TestLimited: a function ended early")
		}

		// The 5th job cannot run: all 4 limited slots are occupied. Once the bubble settles, the
		// 5th submit is durably blocked acquiring a slot, so its function has not run.
		synctest.Wait()
		select {
		case <-blockedHappened:
			t.Fatalf("TestLimited: a function that should have been blocked wasn't")
		default:
		}

		// Free exactly one slot: one original job finishes, which lets the 5th job run.
		exit <- struct{}{}

		synctest.Wait()
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

		synctest.Wait()
		if done.Load() != 4 {
			t.Fatalf("TestLimited: all goroutines should have finished")
		}
	})
}
