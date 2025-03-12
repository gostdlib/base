package worker

import (
	"context"
	"sync"
	"time"

	bSync "github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/telemetry/otel/trace/span"
	"go.opentelemetry.io/otel/attribute"
)

// Limited creates a Limited pool from the Pool. "size" is the number of goroutines that can execute concurrently.
func (p *Pool) Limited(size int) *Limited {
	if size < 1 {
		panic("cannot have a Limited Pool with size < 1")
	}

	ch := make(chan struct{}, size)
	return &Limited{p: p, limit: ch}
}

// Limited is a worker pool that limits the number of concurrent jobs that can be run.
// This can be created from a Pool using the Limited() method.
type Limited struct {
	p     *Pool
	wg    sync.WaitGroup
	limit chan struct{}
}

// Submit submits function f to be run. Context can be cancelled before submit, however if the function is
// already submitted it is the responsibility of the function to honor/not honor cancellation.
func (l *Limited) Submit(ctx context.Context, f func()) error {
	spanner := span.Get(ctx)

	t := time.Now()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case l.limit <- struct{}{}:
	}

	since := time.Since(t)
	spanner.Event(
		"worker.Pool:Limited.Submit()",
		attribute.Int64("block_duration_ns", int64(since)),
	)
	l.wg.Add(1)

	wrap := func() {
		defer func() {
			<-l.limit
			l.wg.Done()
		}()

		f()
	}
	return l.p.Submit(ctx, wrap)
}

// Wait will wait for all goroutines in the pool to finish.
func (l *Limited) Wait() {
	l.wg.Wait()
}

// Group returns a Group that can be used to spin off goroutines and then wait for them to finish.
// This will use the Limited pool to limit the number of concurrent goroutines. Safer than a sync.WaitGroup.
func (l *Limited) Group() bSync.Group {
	return bSync.Group{Pool: l}
}
