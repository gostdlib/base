package queue

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// TestBboltBoundedDrainWakesProducers is a regression test for a lost-wakeup on the
// bounded on-disk backing. Push admits on count+pending (committed + buffered), but Pop
// only re-signaled notFullCh when count == maxSize exactly. Because buffered-but-
// uncommitted items live in pending, count can sit below maxSize while the queue is
// logically full, so a Pop that frees real capacity would not wake a producer parked at
// the admission gate and the run would deadlock. Concurrent producers + a draining
// consumer on a small bounded bbolt queue must deliver every item with no hang.
func TestBboltBoundedDrainWakesProducers(t *testing.T) {
	const (
		maxSize   = 3
		producers = 6
		perProd   = 40
		total     = producers * perProd
	)
	for _, m := range queueMakers() {
		if !isBbolt(m.name) {
			continue
		}
		func() {
			baseCtx := t.Context()
			q := m.make(t, baseCtx, maxSize)

			results := make(chan int, total)
			prodErr := make(chan error, producers)
			for g := 0; g < producers; g++ {
				go func(base int) {
					for i := 0; i < perProd; i++ {
						if _, err := q.Push(baseCtx, []Number[int]{m.item(base + i)}); err != nil {
							prodErr <- err
							return
						}
					}
					prodErr <- nil
				}(g * perProd)
			}

			cctx, ccancel := context.WithCancel(baseCtx)
			go func() {
				for {
					items, err := q.Pop(cctx, maxSize)
					if err != nil {
						return
					}
					for _, it := range items {
						results <- it.V
					}
				}
			}()

			got := 0
			deadline := time.After(20 * time.Second)
			for got < total {
				select {
				case <-results:
					got++
				case <-deadline:
					ccancel()
					t.Fatalf("TestBboltBoundedDrainWakesProducers(%s): timed out with %d/%d items (producer parked at the admission gate was never woken)", m.name, got, total)
				}
			}
			ccancel()
			for g := 0; g < producers; g++ {
				if err := <-prodErr; err != nil {
					t.Errorf("TestBboltBoundedDrainWakesProducers(%s): producer got err == %s, want err == nil", m.name, err)
				}
			}
			if err := q.Close(baseCtx); err != nil {
				t.Errorf("TestBboltBoundedDrainWakesProducers(%s): Close got err == %s, want err == nil", m.name, err)
			}
		}()
	}
}

// slowDecode is a WithCodec decoder that sleeps so the cost of decoding the iteration
// remainder is observable: if RangeAllCOW decodes the remainder while holding the read
// lock, a concurrent writer is blocked for the whole decode; if it snapshots and releases
// first, the writer proceeds immediately.
func slowDecode(src []byte, dst *Number[int]) error {
	time.Sleep(15 * time.Millisecond)
	return JSONDecode(src, dst)
}

// TestBboltRangeAllCOWReleasesLock is a regression test for RangeAllCOW on the on-disk
// backing not honoring its copy-on-write contract: once a writer is waiting it must
// snapshot the remainder and release the read lock so the writer proceeds, then finish
// iterating from the copy. Previously bbolt held the read lock across the entire
// (decode-heavy) scan, blocking the writer for the whole iteration.
func TestBboltRangeAllCOWReleasesLock(t *testing.T) {
	const items = 8
	ctx := t.Context()
	b, err := NewBboltFIFO[Number[int]](
		ctx,
		diskRoot(t),
		WithCodec(func(dst *bytes.Buffer, v Number[int]) error { return JSONEncode(dst, v) }, slowDecode),
	)
	if err != nil {
		t.Fatalf("TestBboltRangeAllCOWReleasesLock: NewBboltFIFO got err == %s, want err == nil", err)
	}
	q, err := New[Number[int]](ctx, "test", b, Unlimited)
	if err != nil {
		t.Fatalf("TestBboltRangeAllCOWReleasesLock: New got err == %s, want err == nil", err)
	}
	for i := 0; i < items; i++ {
		if ok, err := q.Push(ctx, []Number[int]{fifoItem(i)}); err != nil || !ok {
			t.Fatalf("TestBboltRangeAllCOWReleasesLock: Push(%d) got (ok=%v err=%v), want (true,nil)", i, ok, err)
		}
	}

	pushed := make(chan time.Time, 1)
	start := time.Now()
	seen := 0
	for _, err := range q.RangeAllCOW(ctx) {
		if err != nil {
			t.Fatalf("TestBboltRangeAllCOWReleasesLock: iteration got err == %s, want err == nil", err)
		}
		if seen == 0 {
			pushStarted := make(chan struct{})
			go func() {
				close(pushStarted) // about to enter Push (which then blocks on writeWanted)
				_, _ = q.Push(ctx, []Number[int]{fifoItem(999)})
				pushed <- time.Now()
			}()
			// Wait for the worker to reach Push before sleeping; if the scheduler
			// delayed the goroutine until after the iteration finished, the writer
			// would never have been pending mid-scan and a broken COW that holds
			// the lock through the whole decode would still appear to pass.
			<-pushStarted
			time.Sleep(40 * time.Millisecond) // let the writer register as pending
		}
		seen++
	}
	iterDone := time.Now()

	var pushedAt time.Time
	select {
	case pushedAt = <-pushed:
	case <-time.After(10 * time.Second):
		t.Fatalf("TestBboltRangeAllCOWReleasesLock: concurrent Push never completed")
	}

	// Total iteration decodes every item (~items*15ms). If the writer had to wait for
	// the lock until the decode-heavy scan finished, it would unblock at roughly
	// iteration end. Honoring COW, it unblocks well before that.
	writerWaited := pushedAt.Sub(start)
	iterTook := iterDone.Sub(start)
	if writerWaited >= iterTook/2 {
		t.Errorf("TestBboltRangeAllCOWReleasesLock: writer unblocked after %s of a %s iteration; want it to proceed during iteration (COW lock not released)", writerWaited, iterTook)
	}
}

func isBbolt(name string) bool {
	return name == "fifo-bbolt" || name == "fifo-bbolt+index" ||
		name == "priority-bbolt" || name == "priority-bbolt+index"
}
