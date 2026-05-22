package queue

import (
	"testing"
	"time"
)

// TestBboltClearDrainsInFlightPush is a regression test: a Clear concurrent with a
// mid-commit Push must not let items the flusher is about to commit survive in the
// bucket. Previously Clear acquired p.lk (which the flusher releases at the top of
// commit()) and deleted the bucket; the flusher then completed its db.Update against
// the recreated bucket and the item reappeared. The fix routes Clear through the
// single-threaded flusher so it serializes with all in-flight commits.
func TestBboltClearDrainsInFlightPush(t *testing.T) {
	// Block the flusher inside commit(). commit() is called only by the single
	// flusher goroutine, so a plain bool is race-free here.
	started := make(chan struct{})
	release := make(chan struct{})
	first := true

	ctx := t.Context()
	b, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t))
	if err != nil {
		t.Fatalf("TestBboltClearDrainsInFlightPush: NewBboltFIFO got err == %s, want err == nil", err)
	}
	// Install the per-instance hook before the queue is exposed (and thus before any
	// flusher work runs). New (below) is what starts the flusher goroutine.
	b.(*bboltBacking[Number[int]]).hooks.commitStart = func() {
		if first {
			first = false
			close(started)
		}
		<-release
	}
	q, err := New[Number[int]](ctx, "test", b, Unlimited)
	if err != nil {
		t.Fatalf("TestBboltClearDrainsInFlightPush: New got err == %s, want err == nil", err)
	}

	pushErr := make(chan error, 1)
	go func() {
		_, e := q.Push(ctx, []Number[int]{fifoItem(1)})
		pushErr <- e
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("TestBboltClearDrainsInFlightPush: flusher never entered commit")
	}

	clearErr := make(chan error, 1)
	clearStarted := make(chan struct{})
	go func() {
		close(clearStarted) // about to invoke Clear
		clearErr <- q.Clear(ctx)
	}()
	// Wait until the worker has reached Clear, then sleep so a pre-fix Clear that
	// acquires the lock and deletes the bucket synchronously has time to do so
	// before we release the flusher. Without the started barrier the scheduler
	// could delay the worker until after close(release), masking the bug because
	// the late Clear would land after the commit had already re-populated the
	// bucket.
	<-clearStarted
	time.Sleep(50 * time.Millisecond)

	close(release)

	select {
	case e := <-pushErr:
		if e != nil {
			t.Fatalf("TestBboltClearDrainsInFlightPush: Push got err == %s, want err == nil", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TestBboltClearDrainsInFlightPush: Push did not complete")
	}
	select {
	case e := <-clearErr:
		if e != nil {
			t.Fatalf("TestBboltClearDrainsInFlightPush: Clear got err == %s, want err == nil", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TestBboltClearDrainsInFlightPush: Clear did not complete")
	}

	if l := q.Len(); l != 0 {
		t.Errorf("TestBboltClearDrainsInFlightPush: Len after Clear got %d, want 0 (in-flight Push survived Clear)", l)
	}
	if err := q.Close(ctx); err != nil {
		t.Errorf("TestBboltClearDrainsInFlightPush: Close got err == %s, want err == nil", err)
	}
}
