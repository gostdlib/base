package queue

import (
	"context"
	"testing"
	"time"
)

// TestBboltBoundedConcurrentPush is a regression test for concurrent over-admission of a
// bounded on-disk queue. Many Push calls race while their items are still buffered
// (uncommitted), so each can pass an admission gate that only consults the committed
// count. A correct bounded queue must never hold more than maxSize items; the excess
// pushes must block at the gate (and return once their context is canceled).
func TestBboltBoundedConcurrentPush(t *testing.T) {
	const (
		maxSize = 2
		pushers = 32
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	b, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t))
	if err != nil {
		t.Fatalf("TestBboltBoundedConcurrentPush: NewBboltFIFO got err == %s, want err == nil", err)
	}
	q, err := New[Number[int]](ctx, "test", b, maxSize)
	if err != nil {
		t.Fatalf("TestBboltBoundedConcurrentPush: New got err == %s, want err == nil", err)
	}

	for i := 0; i < pushers; i++ {
		go func(v int) { _, _ = q.Push(ctx, []Number[int]{{V: v}}) }(i)
	}

	var maxLen int64
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		if l := q.Len(); l > maxLen {
			maxLen = l
		}
		if maxLen > maxSize {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if maxLen > maxSize {
		t.Errorf("TestBboltBoundedConcurrentPush: queue Len reached %d, want <= %d (bounded maxSize over-admitted under concurrent Push)", maxLen, maxSize)
	}

	cancel() // release any gate-blocked pushers
	if err := q.Close(context.Background()); err != nil {
		t.Errorf("TestBboltBoundedConcurrentPush: Close got err == %s, want err == nil", err)
	}
}
