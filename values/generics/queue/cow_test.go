package queue

import (
	"sort"
	"testing"
)

// TestRangeAllCOWParity verifies RangeAllCOW yields the same items as RangeAll on a
// quiescent queue, across the in-memory backings.
func TestRangeAllCOWParity(t *testing.T) {
	tests := []struct {
		name     string
		priority bool
		backing  func() (Backing[Number[int]], error)
	}{
		{"in-memory FIFO btree", false, func() (Backing[Number[int]], error) { return NewBTreeFIFO[Number[int]]() }},
		{"in-memory priority", true, func() (Backing[Number[int]], error) { return NewBTreePriority[Number[int]]() }},
		{"in-memory FIFO index", false, func() (Backing[Number[int]], error) { return NewBTreeFIFO[Number[int]](WithIndex()) }},
		{"in-memory FIFO btype", false, func() (Backing[Number[int]], error) { return NewBtypeFIFO[Number[int]]() }},
	}

	for _, test := range tests {
		ctx := t.Context()
		backing, err := test.backing()
		if err != nil {
			t.Fatalf("TestRangeAllCOWParity(%s): backing got err == %s, want nil", test.name, err)
		}
		q, err := New[Number[int]](ctx, backing, 0)
		if err != nil {
			t.Fatalf("TestRangeAllCOWParity(%s): New got err == %s, want nil", test.name, err)
		}
		for _, n := range []int{5, 1, 4, 2, 3} {
			if _, err := q.Push(ctx, []Number[int]{itemFor(test.priority, n)}); err != nil {
				t.Fatalf("TestRangeAllCOWParity(%s): Push got err == %s", test.name, err)
			}
		}

		var plain, cow []int
		for v, err := range q.RangeAll(ctx) {
			if err != nil {
				t.Fatalf("TestRangeAllCOWParity(%s): RangeAll got err == %s", test.name, err)
			}
			plain = append(plain, v.V)
		}
		for v, err := range q.RangeAllCOW(ctx) {
			if err != nil {
				t.Fatalf("TestRangeAllCOWParity(%s): RangeAllCOW got err == %s", test.name, err)
			}
			cow = append(cow, v.V)
		}
		if len(plain) != len(cow) {
			t.Fatalf("TestRangeAllCOWParity(%s): RangeAll len %d != RangeAllCOW len %d", test.name, len(plain), len(cow))
		}
		for i := range plain {
			if plain[i] != cow[i] {
				t.Errorf("TestRangeAllCOWParity(%s): item %d: RangeAll=%d RangeAllCOW=%d", test.name, i, plain[i], cow[i])
			}
		}
		if err := q.Close(ctx); err != nil {
			t.Errorf("TestRangeAllCOWParity(%s): Close got err == %s", test.name, err)
		}
	}
}

// TestRangeAllCOWContention verifies that a concurrent writer does not deadlock a
// RangeAllCOW iteration and that the iteration observes a consistent snapshot (the item
// pushed by the contending writer is not yielded). Correctness holds regardless of
// scheduling: if contention is observed, COW snapshots and releases; if the loop finishes
// first, it simply ranged the original items. Either way: no deadlock, snapshot is the
// pre-contention set, and the late Push lands.
func TestRangeAllCOWContention(t *testing.T) {
	ctx := t.Context()
	backing, err := NewBTreeFIFO[Number[int]]()
	if err != nil {
		t.Fatalf("TestRangeAllCOWContention: NewBTreeFIFO got err == %s, want nil", err)
	}
	q, err := New[Number[int]](ctx, backing, 0)
	if err != nil {
		t.Fatalf("TestRangeAllCOWContention: New got err == %s, want nil", err)
	}
	const n = 200
	for i := 0; i < n; i++ {
		if _, err := q.Push(ctx, []Number[int]{fifoItem(i)}); err != nil {
			t.Fatalf("TestRangeAllCOWContention: Push(%d) got err == %s", i, err)
		}
	}

	release := make(chan struct{})
	bDone := make(chan error, 1)
	go func() {
		<-release
		_, err := q.Push(ctx, []Number[int]{fifoItem(10_000)})
		bDone <- err
	}()

	var got []int
	first := true
	for v, err := range q.RangeAllCOW(ctx) {
		if err != nil {
			t.Fatalf("TestRangeAllCOWContention: RangeAllCOW got err == %s", err)
		}
		if first {
			close(release) // trigger the concurrent writer mid-iteration
			first = false
		}
		got = append(got, v.V)
	}

	if err := <-bDone; err != nil {
		t.Fatalf("TestRangeAllCOWContention: concurrent Push got err == %s, want nil", err)
	}

	sort.Ints(got)
	if len(got) != n {
		t.Fatalf("TestRangeAllCOWContention: snapshot len got %d, want %d", len(got), n)
	}
	for i := 0; i < n; i++ {
		if got[i] != i {
			t.Fatalf("TestRangeAllCOWContention: snapshot item %d got %d, want %d", i, got[i], i)
			break
		}
	}
	if l := q.Len(); l != int64(n+1) {
		t.Errorf("TestRangeAllCOWContention: Len after concurrent Push got %d, want %d", l, n+1)
	}
	if err := q.Close(ctx); err != nil {
		t.Errorf("TestRangeAllCOWContention: Close got err == %s", err)
	}
}
