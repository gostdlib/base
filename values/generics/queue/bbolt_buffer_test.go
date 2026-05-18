package queue

import (
	"errors"
	"testing"
)

// TestBboltBufferedFlush exercises the on-disk group-commit buffer: a batch larger than
// the buffer cap is rejected, a batch at the cap commits and is poppable (proving push
// blocked until the flusher committed), and a small batch is flushed by the timer.
func TestBboltBufferedFlush(t *testing.T) {
	ctx := t.Context()
	const maxB = 500
	backing, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t))
	if err != nil {
		t.Fatalf("TestBboltBufferedFlush: NewBboltFIFO got err == %s, want nil", err)
	}
	q, err := New[Number[int]](ctx, backing, 0, WithMaxBatch(maxB))
	if err != nil {
		t.Fatalf("TestBboltBufferedFlush: New got err == %s, want nil", err)
	}

	// Batch larger than the configured max batch can never fit.
	over := make([]Number[int], maxB+1)
	if _, err := q.Push(ctx, over); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("TestBboltBufferedFlush: Push(%d items) got err == %v, want ErrBatchTooLarge", len(over), err)
	}

	// A full-cap batch must commit (push returns only after the flush) and be poppable.
	full := make([]Number[int], maxB)
	for i := range full {
		full[i] = fifoItem(i)
	}
	if ok, err := q.Push(ctx, full); err != nil || !ok {
		t.Fatalf("TestBboltBufferedFlush: Push(cap) got (ok=%v err=%v), want (true,nil)", ok, err)
	}
	if n := q.Len(); n != int64(maxB) {
		t.Errorf("TestBboltBufferedFlush: Len after cap push got %d, want %d", n, maxB)
	}
	got := popN(t, ctx, "bbolt-buffered", q, maxB)
	for i, v := range got {
		if v != i {
			t.Fatalf("TestBboltBufferedFlush: pop[%d] got %d, want %d", i, v, i)
		}
	}

	// A small batch must still be committed (by the interval timer) and become poppable.
	if _, err := q.Push(ctx, []Number[int]{fifoItem(42), fifoItem(7)}); err != nil {
		t.Fatalf("TestBboltBufferedFlush: small Push got err == %s", err)
	}
	if n := q.Len(); n != 2 {
		t.Errorf("TestBboltBufferedFlush: Len after small push got %d, want 2", n)
	}
	if rem := popN(t, ctx, "bbolt-buffered", q, 2); rem[0] != 42 || rem[1] != 7 {
		t.Errorf("TestBboltBufferedFlush: drained %v, want [42 7]", rem)
	}

	if err := q.Close(ctx); err != nil {
		t.Errorf("TestBboltBufferedFlush: Close got err == %s", err)
	}
}
