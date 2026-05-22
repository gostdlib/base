package queue

// Benchmarks for the per-backing waiter-counter signal gating.
//
// Run before/after comparison:
//
//	cd values/generics/queue
//	git stash push -m gating -- fifo.go heap.go btree.go btype_fifo.go bbolt.go
//	go test -run=^$ -bench='BenchmarkSignal|BenchmarkBoundedPopAllocs' -benchmem -count=10 -timeout 600s . > /tmp/before.txt
//	git stash pop
//	go test -run=^$ -bench='BenchmarkSignal|BenchmarkBoundedPopAllocs' -benchmem -count=10 -timeout 600s . > /tmp/after.txt
//	benchstat /tmp/before.txt /tmp/after.txt
//
// Three scenarios per backing:
//   PopNoWaiter  : steady-state bounded Pop/Push pair, no parked producer.
//                  Headline win: every Pop's notFull signal is gated to a no-op.
//   PopWithWaiter: same shape, plus one permanently-parked producer.
//                  No-regression sanity: signal still fires both pre and post fix.
//   PushWasEmpty : unbounded queue drained to empty each iter so Push's wasEmpty
//                  signal fires; no NotEmpty/Pop waiter is parked, so post-fix
//                  the gate skips.
//
// bbolt only runs PopNoWaiter (PopWithWaiter and PushWasEmpty would be dominated
// by disk wall time, drowning the signal-allocation signal we're measuring).

import (
	"context"
	"os"
	"testing"
)

// signalBackingMaker constructs a Backing for the bench. priority selects the
// pushable item shape (Priority()>0 for priority backings, ==0 for FIFO).
type signalBackingMaker struct {
	name     string
	priority bool
	isBbolt  bool
	build    func(b *testing.B, ctx context.Context) Backing[Number[int]]
}

func signalBackingMakers() []signalBackingMaker {
	return []signalBackingMaker{
		{"fifo-slice", false, false, func(b *testing.B, _ context.Context) Backing[Number[int]] {
			bk, err := NewFIFO[Number[int]]()
			if err != nil {
				b.Fatalf("NewFIFO: %v", err)
			}
			return bk
		}},
		{"fifo-btype", false, false, func(b *testing.B, _ context.Context) Backing[Number[int]] {
			bk, err := newBtypeFIFO[Number[int]]()
			if err != nil {
				b.Fatalf("newBtypeFIFO: %v", err)
			}
			return bk
		}},
		{"fifo-btree", false, false, func(b *testing.B, _ context.Context) Backing[Number[int]] {
			bk, err := NewBTreeFIFO[Number[int]]()
			if err != nil {
				b.Fatalf("NewBTreeFIFO: %v", err)
			}
			return bk
		}},
		{"fifo-btree+index", false, false, func(b *testing.B, _ context.Context) Backing[Number[int]] {
			bk, err := NewBTreeFIFO[Number[int]](WithIndex())
			if err != nil {
				b.Fatalf("NewBTreeFIFO+index: %v", err)
			}
			return bk
		}},
		{"priority-heap", true, false, func(b *testing.B, _ context.Context) Backing[Number[int]] {
			bk, err := NewPriority[Number[int]]()
			if err != nil {
				b.Fatalf("NewPriority: %v", err)
			}
			return bk
		}},
		{"priority-btree", true, false, func(b *testing.B, _ context.Context) Backing[Number[int]] {
			bk, err := NewBTreePriority[Number[int]]()
			if err != nil {
				b.Fatalf("NewBTreePriority: %v", err)
			}
			return bk
		}},
		{"priority-btree+index", true, false, func(b *testing.B, _ context.Context) Backing[Number[int]] {
			bk, err := NewBTreePriority[Number[int]](WithIndex())
			if err != nil {
				b.Fatalf("NewBTreePriority+index: %v", err)
			}
			return bk
		}},
		{"fifo-bbolt", false, true, func(b *testing.B, ctx context.Context) Backing[Number[int]] {
			root, err := os.OpenRoot(b.TempDir())
			if err != nil {
				b.Fatalf("os.OpenRoot: %v", err)
			}
			bk, err := NewBboltFIFO[Number[int]](ctx, root, WithNoSync())
			if err != nil {
				b.Fatalf("NewBboltFIFO: %v", err)
			}
			return bk
		}},
	}
}

func itemForBench(priority bool, v int) Number[int] {
	if priority {
		return prioItem(v)
	}
	return fifoItem(v)
}

// BenchmarkSignal is the parameterized matrix described at the top of this file.
func BenchmarkSignal(b *testing.B) {
	for _, m := range signalBackingMakers() {
		b.Run(m.name, func(b *testing.B) {
			b.Run("PopNoWaiter", func(b *testing.B) { benchPopNoWaiter(b, m) })
			if !m.isBbolt {
				b.Run("PopWithWaiter", func(b *testing.B) { benchPopWithWaiter(b, m) })
				b.Run("PushWasEmpty", func(b *testing.B) { benchPushWasEmpty(b, m) })
			}
		})
	}
}

// benchPopNoWaiter: bounded queue seeded to half-full, then a tight Push+Pop loop
// keeps occupancy oscillating between maxSize/2 and maxSize/2+1. Queue never
// reaches maxSize and never reaches 0, so no producer or consumer parks.
// Per iteration: Pop frees one slot; pre-fix unconditionally resetSignal'd
// notFullCh (1 chan alloc), post-fix gates it on notFullWaiters > 0 (0 allocs).
func benchPopNoWaiter(b *testing.B, m signalBackingMaker) {
	const maxSize = 16
	ctx := b.Context()
	bk := m.build(b, ctx)
	q, err := New[Number[int]](ctx, "", bk, maxSize)
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	for i := 0; i < maxSize/2; i++ {
		if _, err := q.Push(ctx, []Number[int]{itemForBench(m.priority, i)}); err != nil {
			b.Fatalf("seed Push: %v", err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := q.Push(ctx, []Number[int]{itemForBench(m.priority, maxSize+i)}); err != nil {
			b.Fatalf("Push: %v", err)
		}
		if _, err := q.Pop(ctx, 1); err != nil {
			b.Fatalf("Pop: %v", err)
		}
	}
	b.StopTimer()
	q.Close(ctx)
}

// benchPopWithWaiter: bounded queue filled to maxSize, plus a background producer
// permanently parked on notFullCh trying to push a batch larger than what the
// per-iteration Pop frees, so it wakes and re-parks each iteration. Every Pop
// reaches resetSignal both pre and post fix (notFullWaiters > 0). Delta should
// be ≈ 0; this guards against accidentally penalizing the with-waiter path.
func benchPopWithWaiter(b *testing.B, m signalBackingMaker) {
	const maxSize = 8
	ctx, cancel := context.WithCancel(b.Context())
	defer cancel()
	bk := m.build(b, ctx)
	q, err := New[Number[int]](ctx, "", bk, maxSize)
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	for i := 0; i < maxSize; i++ {
		if _, err := q.Push(ctx, []Number[int]{itemForBench(m.priority, i)}); err != nil {
			b.Fatalf("seed Push: %v", err)
		}
	}
	// The parker pushes a batch of 4. The bench loop's Pop(1)+Push(1) frees and
	// refills exactly one slot, so the parker (which needs 4 free slots) never
	// fits and stays parked, modulo the brief wake+re-check each Pop signals.
	parkerDone := make(chan struct{})
	go func() {
		defer close(parkerDone)
		batch := make([]Number[int], 4)
		for i := range batch {
			batch[i] = itemForBench(m.priority, maxSize+100+i)
		}
		for {
			if _, err := q.Push(ctx, batch); err != nil {
				return // ErrClosed or ctx.Cause on shutdown
			}
		}
	}()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := q.Pop(ctx, 1); err != nil {
			b.Fatalf("Pop: %v", err)
		}
		if _, err := q.Push(ctx, []Number[int]{itemForBench(m.priority, maxSize+i)}); err != nil {
			b.Fatalf("Push: %v", err)
		}
	}
	b.StopTimer()
	cancel()
	q.Close(context.Background())
	<-parkerDone
}

// benchPushWasEmpty: unbounded queue, repeatedly Push 1 then Pop 1 so the queue
// is empty at the start of every iteration's Push. wasEmpty fires every Push;
// no consumer is parked (Pop sees the item immediately), so notEmptyWaiters
// stays 0. Pre-fix resetSignal(&notEmptyCh) every Push (1 chan alloc); post-fix
// the gate skips it (0 allocs). Pop's notFull signal is a no-op for unbounded.
func benchPushWasEmpty(b *testing.B, m signalBackingMaker) {
	ctx := b.Context()
	bk := m.build(b, ctx)
	q, err := New[Number[int]](ctx, "", bk, Unlimited)
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := q.Push(ctx, []Number[int]{itemForBench(m.priority, i)}); err != nil {
			b.Fatalf("Push: %v", err)
		}
		if _, err := q.Pop(ctx, 1); err != nil {
			b.Fatalf("Pop: %v", err)
		}
	}
	b.StopTimer()
	q.Close(ctx)
}
