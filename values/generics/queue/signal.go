package queue

import (
	"sync/atomic"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/context"
)

// signal allows signalling a series of waiters to an event without using channels that must be closed.
type signal struct {
	waiters atomic.Int32
	mu      sync.RWMutex
	signals *sync.Pool[chan struct{}]
}

// newSignal creates a new signal instance. poolSize is the amount of always available buffer for the sync.Pool.
func newSignal(ctx context.Context, poolSize int) *signal {
	p := sync.NewPool(ctx, "", func() chan struct{} { return make(chan struct{}, 1) }, sync.WithBuffer(poolSize))
	s := &signal{signals: p}
	s.mu.Lock()
	return s
}

// Wait for a signal. If ctx is cancelled, context.Cause() will be the error returned.
func (s *signal) Wait(ctx context.Context) error {
	s.waiters.Add(1)
	ch := s.signals.Get(ctx)
	func() {
		context.Pool(ctx).Submit(
			ctx,
			func() {
				s.mu.RLock()
				s.mu.RUnlock() // This is not a mistake.
				s.waiters.Add(-1)
				ch <- struct{}{}
			},
		)
	}()

	select {
	case <-ctx.Done():
		context.Pool(ctx).Submit(
			ctx,
			func() {
				<-ch
				s.signals.Put(ctx, ch)
			})
		return context.Cause(ctx)
	case <-ch:
		s.signals.Put(ctx, ch)
		return nil
	}
}

// Signal all the waiters and returns once all waiters have released. The Signal is
// ready to use again.
func (s *signal) Signal() {
	s.mu.Unlock()
	for {
		if s.waiters.Load() == 0 {
			s.mu.Lock()
			if s.waiters.Load() == 0 {
				return
			}
			s.mu.Unlock()
		}
	}
}
