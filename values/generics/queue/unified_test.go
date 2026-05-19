package queue

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kylelemons/godebug/pretty"
)

// qMaker builds one Queue[Number[int]] backing variant at a chosen maxSize. It is the
// shared matrix the unified scenario tests (sequential, empty, full, concurrent) run
// against, so every backing/index/kind combination gets the same coverage.
type qMaker struct {
	name     string
	priority bool
	make     func(t *testing.T, ctx context.Context, maxSize int, opts ...Option) *Queue[Number[int]]
}

// item builds a pushable item for this maker's kind (priority backings need Priority()>0,
// FIFO backings need Priority()==0).
func (m qMaker) item(v int) Number[int] { return itemFor(m.priority, v) }

func newQ(t *testing.T, b Backing[Number[int]], err error, ctx context.Context, maxSize int, opts ...Option) *Queue[Number[int]] {
	t.Helper()
	if err != nil {
		t.Fatalf("backing build got err == %s, want err == nil", err)
	}
	q, err := New[Number[int]](ctx, "test", b, maxSize, opts...)
	if err != nil {
		t.Fatalf("New got err == %s, want err == nil", err)
	}
	return q
}

func queueMakers() []qMaker {
	return []qMaker{
		{"fifo-slice", false, func(t *testing.T, ctx context.Context, max int, o ...Option) *Queue[Number[int]] {
			b, err := NewFIFO[Number[int]]()
			return newQ(t, b, err, ctx, max, o...)
		}},
		{"fifo-btree", false, func(t *testing.T, ctx context.Context, max int, o ...Option) *Queue[Number[int]] {
			b, err := NewBTreeFIFO[Number[int]]()
			return newQ(t, b, err, ctx, max, o...)
		}},
		{"fifo-btype", false, func(t *testing.T, ctx context.Context, max int, o ...Option) *Queue[Number[int]] {
			b, err := newBtypeFIFO[Number[int]]()
			return newQ(t, b, err, ctx, max, o...)
		}},
		{"fifo-btree+index", false, func(t *testing.T, ctx context.Context, max int, o ...Option) *Queue[Number[int]] {
			b, err := NewBTreeFIFO[Number[int]](WithIndex())
			return newQ(t, b, err, ctx, max, o...)
		}},
		{"priority-heap", true, func(t *testing.T, ctx context.Context, max int, o ...Option) *Queue[Number[int]] {
			b, err := NewPriority[Number[int]]()
			return newQ(t, b, err, ctx, max, o...)
		}},
		{"priority-btree", true, func(t *testing.T, ctx context.Context, max int, o ...Option) *Queue[Number[int]] {
			b, err := NewBTreePriority[Number[int]]()
			return newQ(t, b, err, ctx, max, o...)
		}},
		{"priority-btree+index", true, func(t *testing.T, ctx context.Context, max int, o ...Option) *Queue[Number[int]] {
			b, err := NewBTreePriority[Number[int]](WithIndex())
			return newQ(t, b, err, ctx, max, o...)
		}},
		{"fifo-bbolt", false, func(t *testing.T, ctx context.Context, max int, o ...Option) *Queue[Number[int]] {
			b, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t))
			return newQ(t, b, err, ctx, max, o...)
		}},
		{"fifo-bbolt+index", false, func(t *testing.T, ctx context.Context, max int, o ...Option) *Queue[Number[int]] {
			b, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t), WithIndex())
			return newQ(t, b, err, ctx, max, o...)
		}},
		{"priority-bbolt", true, func(t *testing.T, ctx context.Context, max int, o ...Option) *Queue[Number[int]] {
			b, err := NewBboltPriority[Number[int]](ctx, diskRoot(t))
			return newQ(t, b, err, ctx, max, o...)
		}},
		{"priority-bbolt+index", true, func(t *testing.T, ctx context.Context, max int, o ...Option) *Queue[Number[int]] {
			b, err := NewBboltPriority[Number[int]](ctx, diskRoot(t), WithIndex())
			return newQ(t, b, err, ctx, max, o...)
		}},
	}
}

// TestQueueSequential pushes a shuffled batch into an unbounded queue and verifies the
// drain order: insertion order for FIFO, ascending priority for priority backings.
func TestQueueSequential(t *testing.T) {
	pushed := []int{5, 3, 9, 1, 7, 2, 8, 0, 6, 4}
	for _, m := range queueMakers() {
		ctx := t.Context()
		q := m.make(t, ctx, 0)

		for _, n := range pushed {
			ok, err := q.Push(ctx, []Number[int]{m.item(n)})
			switch {
			case err != nil:
				t.Fatalf("TestQueueSequential(%s): Push(%d) got err == %s, want err == nil", m.name, n, err)
			case !ok:
				t.Fatalf("TestQueueSequential(%s): Push(%d) got ok == false, want true", m.name, n)
			}
		}
		if got := q.Len(); got != int64(len(pushed)) {
			t.Errorf("TestQueueSequential(%s): Len got %d, want %d", m.name, got, len(pushed))
		}

		want := wantOrder(m.priority, pushed)
		got := pop(t, ctx, m.name, q, len(want))
		if diff := pretty.Compare(want, got); diff != "" {
			t.Errorf("TestQueueSequential(%s): drain order -want +got:\n%s", m.name, diff)
		}
		if got := q.Len(); got != 0 {
			t.Errorf("TestQueueSequential(%s): Len after drain got %d, want 0", m.name, got)
		}
		if err := q.Close(ctx); err != nil {
			t.Errorf("TestQueueSequential(%s): Close got err == %s, want err == nil", m.name, err)
		}
	}
}

// TestQueueEmpty verifies empty-queue behavior: Peek reports not-found, a blocked Pop /
// NotEmpty returns an error when its context is canceled, and a Pop blocked on an empty
// queue unblocks when an item is pushed concurrently.
func TestQueueEmpty(t *testing.T) {
	for _, m := range queueMakers() {
		ctx := t.Context()
		q := m.make(t, ctx, 0)

		if v, ok, err := q.Peek(ctx); err != nil || ok || v.V != 0 {
			t.Errorf("TestQueueEmpty(%s): Peek on empty got (v=%v ok=%v err=%v), want (zero,false,nil)", m.name, v, ok, err)
		}

		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if items, err := q.Pop(cctx, 1); err == nil || items != nil {
			t.Errorf("TestQueueEmpty(%s): Pop on empty+canceled got (items=%v err=%v), want (nil, err)", m.name, items, err)
		}
		if err := q.NotEmpty(cctx); err == nil {
			t.Errorf("TestQueueEmpty(%s): NotEmpty on empty+canceled got err == nil, want err != nil", m.name)
		}

		// A Pop blocked on the empty queue must unblock once an item is pushed.
		type result struct {
			v   int
			err error
		}
		got := make(chan result, 1)
		go func() {
			items, err := q.Pop(ctx, 1)
			r := result{err: err}
			if err == nil && len(items) == 1 {
				r.v = items[0].V
			}
			got <- r
		}()
		// Pop on an empty (non-canceled) queue must block, not return: after a grace
		// period it must still be pending. This deterministically catches a spurious
		// return on empty even if the goroutine raced ahead of this check.
		time.Sleep(50 * time.Millisecond)
		select {
		case r := <-got:
			t.Fatalf("TestQueueEmpty(%s): Pop returned (v=%d err=%v) on an empty queue before any Push", m.name, r.v, r.err)
		default:
		}
		if ok, err := q.Push(ctx, []Number[int]{m.item(42)}); err != nil || !ok {
			t.Fatalf("TestQueueEmpty(%s): Push got (ok=%v err=%v), want (true,nil)", m.name, ok, err)
		}
		select {
		case r := <-got:
			switch {
			case r.err != nil:
				t.Errorf("TestQueueEmpty(%s): blocked Pop got err == %s, want err == nil", m.name, r.err)
			case r.v != 42:
				t.Errorf("TestQueueEmpty(%s): blocked Pop got %d, want 42", m.name, r.v)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("TestQueueEmpty(%s): Pop did not unblock after Push", m.name)
		}

		if err := q.Close(ctx); err != nil {
			t.Errorf("TestQueueEmpty(%s): Close got err == %s, want err == nil", m.name, err)
		}
	}
}

// TestQueueFull verifies bounded-queue behavior: a full queue blocks Push until space is
// made (or the context is canceled), NotFull errors on a canceled context when full, and
// a batch larger than the bound returns ErrBatchTooLarge.
func TestQueueFull(t *testing.T) {
	const maxSize = 5
	for _, m := range queueMakers() {
		ctx := t.Context()
		q := m.make(t, ctx, maxSize)

		for i := 0; i < maxSize; i++ {
			if ok, err := q.Push(ctx, []Number[int]{m.item(i)}); err != nil || !ok {
				t.Fatalf("TestQueueFull(%s): fill Push(%d) got (ok=%v err=%v), want (true,nil)", m.name, i, ok, err)
			}
		}
		if got := q.Len(); got != maxSize {
			t.Fatalf("TestQueueFull(%s): Len after fill got %d, want %d", m.name, got, maxSize)
		}

		// A batch that can never fit the bound is rejected.
		over := make([]Number[int], maxSize+1)
		for i := range over {
			over[i] = m.item(100 + i)
		}
		if _, err := q.Push(ctx, over); !errors.Is(err, ErrBatchTooLarge) {
			t.Errorf("TestQueueFull(%s): Push(oversized) got err == %v, want ErrBatchTooLarge", m.name, err)
		}

		// NotFull on a full queue with a canceled context returns an error.
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if err := q.NotFull(cctx); err == nil {
			t.Errorf("TestQueueFull(%s): NotFull on full+canceled got err == nil, want err != nil", m.name)
		}

		// Push onto the full queue blocks; a canceled context unblocks it with an error
		// and the item is not added.
		tctx, tcancel := context.WithTimeout(ctx, 100*time.Millisecond)
		ok, err := q.Push(tctx, []Number[int]{m.item(999)})
		tcancel()
		if err == nil || ok {
			t.Errorf("TestQueueFull(%s): Push on full+timeout got (ok=%v err=%v), want (false, err)", m.name, ok, err)
		}
		if got := q.Len(); got != maxSize {
			t.Errorf("TestQueueFull(%s): Len after blocked Push got %d, want %d", m.name, got, maxSize)
		}

		// Make room, then a Push succeeds.
		if _, err := q.Pop(ctx, 1); err != nil {
			t.Fatalf("TestQueueFull(%s): Pop got err == %s, want err == nil", m.name, err)
		}
		if ok, err := q.Push(ctx, []Number[int]{m.item(7)}); err != nil || !ok {
			t.Errorf("TestQueueFull(%s): Push after Pop got (ok=%v err=%v), want (true,nil)", m.name, ok, err)
		}

		if err := q.Close(ctx); err != nil {
			t.Errorf("TestQueueFull(%s): Close got err == %s, want err == nil", m.name, err)
		}
	}
}

// TestQueueConcurrent runs many producers and consumers against both an unbounded queue
// and a small bounded one (forcing backpressure), asserting every pushed value is
// delivered exactly once with no loss, duplication, or deadlock.
func TestQueueConcurrent(t *testing.T) {
	const (
		producers = 4
		perProd   = 75
		total     = producers * perProd
	)
	// consumers=1 is the concurrent-fillers + single-drain case; consumers=4 is the
	// concurrent-fillers + multiple-drains case. Both must deliver every item exactly
	// once with no loss, duplication, or deadlock.
	for _, m := range queueMakers() {
		for _, maxSize := range []int{0, 16} {
			// Bounded backpressure semantics are identical across backings and fully
			// covered by the in-memory backings; the on-disk bounded combo only adds
			// slow disk/group-commit wall time, so skip it.
			if maxSize != 0 && strings.Contains(m.name, "bbolt") {
				continue
			}
			for _, consumers := range []int{1, 4} {
				ctx := t.Context()
				q := m.make(t, ctx, maxSize)

				results := make(chan int, total)
				prodErr := make(chan error, producers)
				for g := 0; g < producers; g++ {
					go func(base int) {
						for i := 0; i < perProd; i++ {
							if _, err := q.Push(ctx, []Number[int]{m.item(base + i)}); err != nil {
								prodErr <- err
								return
							}
						}
						prodErr <- nil
					}(g * perProd)
				}

				cctx, ccancel := context.WithCancel(ctx)
				for c := 0; c < consumers; c++ {
					go func() {
						for {
							items, err := q.Pop(cctx, 8)
							if err != nil {
								return // context canceled: all items already collected
							}
							for _, it := range items {
								results <- it.V
							}
						}
					}()
				}

				got := make([]int, 0, total)
				deadline := time.After(60 * time.Second)
				for len(got) < total {
					select {
					case v := <-results:
						got = append(got, v)
					case <-deadline:
						ccancel()
						t.Fatalf("TestQueueConcurrent(%s,max=%d,consumers=%d): timed out with %d/%d items", m.name, maxSize, consumers, len(got), total)
					}
				}
				ccancel()

				for g := 0; g < producers; g++ {
					if err := <-prodErr; err != nil {
						t.Errorf("TestQueueConcurrent(%s,max=%d,consumers=%d): producer got err == %s, want err == nil", m.name, maxSize, consumers, err)
					}
				}

				sort.Ints(got)
				bad := false
				for i := 0; i < total; i++ {
					if got[i] != i {
						bad = true
						break
					}
				}
				if bad {
					t.Errorf("TestQueueConcurrent(%s,max=%d,consumers=%d): delivered multiset != [0,%d)", m.name, maxSize, consumers, total)
				}
				if l := q.Len(); l != 0 {
					t.Errorf("TestQueueConcurrent(%s,max=%d,consumers=%d): Len after drain got %d, want 0", m.name, maxSize, consumers, l)
				}
				if err := q.Close(ctx); err != nil {
					t.Errorf("TestQueueConcurrent(%s,max=%d,consumers=%d): Close got err == %s, want err == nil", m.name, maxSize, consumers, err)
				}
			}
		}
	}
}
