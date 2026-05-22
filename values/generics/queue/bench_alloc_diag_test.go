package queue

// Diagnostic benchmarks for narrowing down where the 4 allocs/op in
// BenchmarkSignalPrimPingPong come from. Each isolates one cost so we can
// see whether the alloc is from the pool, AfterFunc, the bound method
// value, or the consumer side of the test rig.

import (
	"context"
	"testing"

	gctx "github.com/gostdlib/base/context"
	"github.com/gostdlib/base/values/generics/queue/internal/parker"
)

// BenchmarkDiagPoolGetPut: round-trips through the gostdlib pool to see
// whether sync.Pool[*Parker] + WithBuffer(64) round-trips allocation-free.
func BenchmarkDiagPoolGetPut(b *testing.B) {
	w := parker.New()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := w.Register()
		p.Release()
	}
}

// BenchmarkDiagAfterFuncCancel: just context.AfterFunc + cancel, no
// parker involved. Measures the raw AfterFunc registration cost.
func BenchmarkDiagAfterFuncCancel(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fn := func() {}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := gctx.AfterFunc(ctx, fn)
		c()
	}
}

// BenchmarkDiagAfterFuncBoundMethod: AfterFunc with a *Parker method
// value, so we see whether the method-value escape causes an alloc on top
// of AfterFunc's internal alloc.
func BenchmarkDiagAfterFuncBoundMethod(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := parker.New()
	p := w.Register()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := gctx.AfterFunc(ctx, p.Wake)
		c()
	}
	b.StopTimer()
	p.Release()
}

// BenchmarkSignalPrimPingPongBgCtx: PingPong but with context.Background
// so signal.Wait takes its no-watcher branch. Subtracts the AfterFunc /
// method-value overhead from the headline PingPong number.
func BenchmarkSignalPrimPingPongBgCtx(b *testing.B) {
	s := newSignal()
	ctx := gctx.Background()

	reply := make(chan struct{}, 1)
	stop := make(chan struct{})

	go func() {
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

	for !s.HasWaiters() {
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Signal()
		<-reply
		for !s.HasWaiters() {
		}
	}
	b.StopTimer()
	close(stop)
	s.Signal()
}
