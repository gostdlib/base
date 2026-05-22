package queue

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestSignalRaceMesaWait is a regression test for the Mesa-wait lost-wakeup race in the
// queue's signal-based wake mechanism. If signal.Wait ever stops invoking its unlock
// callback only AFTER it has synchronously registered as a waiter, a Signal that runs
// entirely between "release p.lk" and "enter Wait" would see waiters==0, complete as a
// no-op, and the producer would park forever.
//
// The test exercises the path without any waiter-count polling crutch: N producer
// goroutines and N consumer goroutines each run many iterations against a bounded
// maxSize=1 queue across every in-memory backing, asserting all complete (no hang)
// within a hard timeout. A regression would surface as a clean test failure on the
// 30s deadline.
func TestSignalRaceMesaWait(t *testing.T) {
	const (
		producers = 8
		consumers = 8
		perProd   = 200
		total     = producers * perProd
	)
	for _, m := range queueMakers() {
		// Skip bbolt — its commit pipeline is dominated by disk wall time and the
		// in-memory backings already exercise the signal wake path identically.
		if isBbolt(m.name) {
			continue
		}
		func() {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			q := m.make(t, ctx, 1)

			var wg sync.WaitGroup
			received := make(chan int, total)

			wg.Add(consumers)
			for c := 0; c < consumers; c++ {
				go func() {
					defer wg.Done()
					for {
						items, err := q.Pop(ctx, 1)
						if err != nil {
							return
						}
						for _, it := range items {
							received <- it.V
						}
					}
				}()
			}

			prodErr := make(chan error, producers)
			for p := 0; p < producers; p++ {
				go func(base int) {
					for i := 0; i < perProd; i++ {
						if _, err := q.Push(ctx, []Number[int]{m.item(base + i)}); err != nil {
							prodErr <- err
							return
						}
					}
					prodErr <- nil
				}(p * perProd)
			}

			got := 0
			deadline := time.After(30 * time.Second)
			for got < total {
				select {
				case <-received:
					got++
				case <-deadline:
					cancel()
					t.Fatalf("TestSignalRaceMesaWait(%s): timed out with %d/%d items (Mesa wait race: producer registered after Signal completed)", m.name, got, total)
				}
			}
			for p := 0; p < producers; p++ {
				if err := <-prodErr; err != nil {
					t.Errorf("TestSignalRaceMesaWait(%s): producer got err == %s, want err == nil", m.name, err)
				}
			}
			cancel()
			wg.Wait()
			if err := q.Close(context.Background()); err != nil {
				t.Errorf("TestSignalRaceMesaWait(%s): Close got err == %s, want err == nil", m.name, err)
			}
		}()
	}
}
