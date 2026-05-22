package queue

// Micro-benchmarks for the signal primitive itself (signal.go), independent
// of the surrounding queue. These exercise the wake mechanism in isolation
// so changes to the broadcast implementation can be measured directly.
//
// Run before/after comparison:
//
//	cd values/generics/queue
//	go test -run=^$ -bench='BenchmarkSignalPrim' -benchmem -count=10 -timeout 300s . > /tmp/sig_before.txt
//	# ... edit signal.go to swap broadcast implementation ...
//	go test -run=^$ -bench='BenchmarkSignalPrim' -benchmem -count=10 -timeout 300s . > /tmp/sig_after.txt
//	benchstat /tmp/sig_before.txt /tmp/sig_after.txt
//
// Scenarios:
//   NoWaiter        : Signal() in a tight loop, no parked waiters. Hot path
//                     for steady-state queues — measures alloc+close cost.
//   HasWaitersGated : if HasWaiters() { Signal() } in a tight loop, no
//                     waiters. The queue's actual hot path.
//   HasWaitersOnly  : just the gate predicate.
//   PingPong        : Wait/Signal round-trip with one consumer goroutine.
//                     Measures end-to-end wake latency.

import (
	"runtime"
	"sync/atomic"
	"testing"
)

func BenchmarkSignalPrimNoWaiter(b *testing.B) {
	s := newSignal()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Signal()
	}
}

func BenchmarkSignalPrimHasWaitersGated(b *testing.B) {
	s := newSignal()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if s.HasWaiters() {
			s.Signal()
		}
	}
}

func BenchmarkSignalPrimHasWaitersOnly(b *testing.B) {
	s := newSignal()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.HasWaiters()
	}
}

// BenchmarkSignalPrimPingPong: a consumer parks on Wait, the bench loop
// Signals, the consumer's Wait returns, and the consumer re-parks. b.N
// iterations equals b.N Signal/Wait round-trips.
func BenchmarkSignalPrimPingPong(b *testing.B) {
	s := newSignal()
	ctx := b.Context()

	// reply chan length 1 — consumer must drain before re-parking, so
	// signaling and reply are strictly serialized one-to-one.
	reply := make(chan struct{}, 1)
	stop := make(chan struct{})
	var consumerDone atomic.Bool

	go func() {
		defer consumerDone.Store(true)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := s.Wait(ctx, func() {}); err != nil {
				return
			}
			select {
			case reply <- struct{}{}:
			case <-stop:
				return
			}
		}
	}()

	// Park the consumer before timing.
	for !s.HasWaiters() {
		runtime.Gosched()
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Signal()
		<-reply
		// Wait for consumer to re-park before next Signal so each Signal
		// finds exactly one waiter, isolating the wake cost.
		for !s.HasWaiters() {
			if consumerDone.Load() {
				b.Fatal("consumer exited mid-bench")
			}
			runtime.Gosched()
		}
	}
	b.StopTimer()
	close(stop)
	s.Signal() // unstick the consumer if it's parked on Wait
}
