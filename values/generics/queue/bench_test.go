package queue

import (
	"context"
	"testing"
)

// benchItems is the working-set size for Del/Exists benchmarks.
const benchItems = 5000

// benchCase is a named backing constructor; the scan case uses the plain B-Tree FIFO and the
// index case the indexed B-Tree FIFO (WithIndex forces a tree backing).
type benchCase struct {
	name    string
	backing func() (Backing[Number[int]], error)
}

var benchCases = []benchCase{
	{"scan", func() (Backing[Number[int]], error) { return NewBTreeFIFO[Number[int]]() }},
	{"index", func() (Backing[Number[int]], error) { return NewBTreeFIFO[Number[int]](WithIndex()) }},
}

func fillQueue(b *testing.B, ctx context.Context, q *Queue[Number[int]], n int) {
	b.Helper()
	for i := 0; i < n; i++ {
		if _, err := q.Push(ctx, []Number[int]{fifoItem(i)}); err != nil {
			b.Fatalf("Push: %v", err)
		}
	}
}

// BenchmarkExists compares Exists with and without WithIndex on an in-memory
// (btree, since WithIndex forces btree) backing vs the scan-based btree.
func BenchmarkExists(b *testing.B) {
	for _, c := range benchCases {
		b.Run(c.name, func(b *testing.B) {
			ctx := b.Context()
			backing, err := c.backing()
			if err != nil {
				b.Fatalf("backing: %v", err)
			}
			q, err := New[Number[int]](ctx, "test", backing, 0)
			if err != nil {
				b.Fatalf("New: %v", err)
			}
			fillQueue(b, ctx, q, benchItems)
			target := queryItem(benchItems - 1) // worst case for scan: last item
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := q.Exists(ctx, target); err != nil {
					b.Fatalf("Exists: %v", err)
				}
			}
			b.StopTimer()
			q.Close(ctx)
		})
	}
}

// BenchmarkDel compares Del of a single matching item with and without WithIndex.
// The queue is refilled each iteration so every Del actually removes something.
func BenchmarkDel(b *testing.B) {
	for _, c := range benchCases {
		b.Run(c.name, func(b *testing.B) {
			ctx := b.Context()
			backing, err := c.backing()
			if err != nil {
				b.Fatalf("backing: %v", err)
			}
			q, err := New[Number[int]](ctx, "test", backing, 0)
			if err != nil {
				b.Fatalf("New: %v", err)
			}
			fillQueue(b, ctx, q, benchItems)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				v := fifoItem(i % benchItems)
				if err := q.Del(ctx, []Number[int]{v}); err != nil {
					b.Fatalf("Del: %v", err)
				}
				b.StopTimer()
				if _, err := q.Push(ctx, []Number[int]{v}); err != nil {
					b.Fatalf("Push: %v", err)
				}
				b.StartTimer()
			}
			b.StopTimer()
			q.Close(ctx)
		})
	}
}

// BenchmarkBoundedPopAllocs measures allocs per Pop on a bounded queue with no parked
// producer. Before the waiter-count gating, every bounded Pop did close()+make() on
// notFullCh — one chan alloc per op. With the gate, no producer means no signal and no
// alloc on the steady-state path. Expectation: 0 allocs/op for the notFull signal.
//
// We use a slice FIFO and pre-Push enough items into a bounded queue so each Pop
// pulls one without ever needing to wait. The loop measures Push+Pop pairs in
// lockstep so the queue never grows or shrinks unbounded; the alloc count attributed
// to Pop is what we care about (the resetSignal-on-notFull path).
func BenchmarkBoundedPopAllocs(b *testing.B) {
	ctx := b.Context()
	backing, err := NewFIFO[Number[int]]()
	if err != nil {
		b.Fatalf("NewFIFO: %v", err)
	}
	q, err := New[Number[int]](ctx, "", backing, 16)
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	if _, err := q.Push(ctx, []Number[int]{fifoItem(0)}); err != nil {
		b.Fatalf("seed Push: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := q.Push(ctx, []Number[int]{fifoItem(i + 1)}); err != nil {
			b.Fatalf("Push: %v", err)
		}
		if _, err := q.Pop(ctx, 1); err != nil {
			b.Fatalf("Pop: %v", err)
		}
	}
	b.StopTimer()
	q.Close(ctx)
}

// BenchmarkPushPop measures the index-maintenance overhead on the hot path.
func BenchmarkPushPop(b *testing.B) {
	for _, c := range benchCases {
		b.Run(c.name, func(b *testing.B) {
			ctx := b.Context()
			backing, err := c.backing()
			if err != nil {
				b.Fatalf("backing: %v", err)
			}
			q, err := New[Number[int]](ctx, "test", backing, 0)
			if err != nil {
				b.Fatalf("New: %v", err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := q.Push(ctx, []Number[int]{fifoItem(i)}); err != nil {
					b.Fatalf("Push: %v", err)
				}
				if _, err := q.Pop(ctx, 1); err != nil {
					b.Fatalf("Pop: %v", err)
				}
			}
			b.StopTimer()
			q.Close(ctx)
		})
	}
}
