package queue

import (
	"context"
	"sort"
	"testing"
	"time"
)

// TestBoundedBatchPushWakesProducers is a regression test for a lost-wakeup that affects
// every backing on a bounded queue with multi-item Push batches. A producer parks at the
// admission gate whenever occupancy+len(vs) > maxSize, which happens while occupancy is
// still below maxSize once len(vs) > 1. The backings only re-signaled notFullCh when
// occupancy was at/over maxSize (wasFull), so a drain that frees capacity without
// occupancy ever hitting maxSize would never wake the parked producer and the run would
// deadlock. With batch size 3 and maxSize 5, occupancy goes 0->3->(park) and a draining
// Pop never sees occupancy == maxSize, so this hangs across fifo/heap/btree/btype/bbolt
// without the fix.
func TestBoundedBatchPushWakesProducers(t *testing.T) {
	const (
		maxSize   = 5
		batchN    = 3
		producers = 4
		perProd   = 30
		total     = producers * perProd * batchN
	)
	for _, m := range queueMakers() {
		func() {
			baseCtx := t.Context()
			q := m.make(t, baseCtx, maxSize)

			results := make(chan int, total)
			prodErr := make(chan error, producers)
			for g := 0; g < producers; g++ {
				go func(base int) {
					for i := 0; i < perProd; i++ {
						batch := make([]Number[int], batchN)
						for j := 0; j < batchN; j++ {
							batch[j] = m.item(base + i*batchN + j)
						}
						if _, err := q.Push(baseCtx, batch); err != nil {
							prodErr <- err
							return
						}
					}
					prodErr <- nil
				}(g * perProd * batchN)
			}

			cctx, ccancel := context.WithCancel(baseCtx)
			go func() {
				for {
					items, err := q.Pop(cctx, 2)
					if err != nil {
						return
					}
					for _, it := range items {
						results <- it.V
					}
				}
			}()

			got := make([]int, 0, total)
			deadline := time.After(20 * time.Second)
			for len(got) < total {
				select {
				case v := <-results:
					got = append(got, v)
				case <-deadline:
					ccancel()
					t.Fatalf("TestBoundedBatchPushWakesProducers(%s): timed out with %d/%d items (producer parked at the admission gate with a multi-item batch was never woken)", m.name, len(got), total)
				}
			}
			ccancel()
			for g := 0; g < producers; g++ {
				if err := <-prodErr; err != nil {
					t.Errorf("TestBoundedBatchPushWakesProducers(%s): producer got err == %s, want err == nil", m.name, err)
				}
			}
			sort.Ints(got)
			for i := 0; i < total; i++ {
				if got[i] != i {
					t.Errorf("TestBoundedBatchPushWakesProducers(%s): delivered multiset != [0,%d) (first mismatch at %d: got %d)", m.name, total, i, got[i])
					break
				}
			}
			if err := q.Close(baseCtx); err != nil {
				t.Errorf("TestBoundedBatchPushWakesProducers(%s): Close got err == %s, want err == nil", m.name, err)
			}
		}()
	}
}
