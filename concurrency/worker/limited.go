package worker

import (
	"context"
	"runtime"
	"sync"
	"time"

	bSync "github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/telemetry/otel/trace/span"
	"go.opentelemetry.io/otel/attribute"
)

var numCPU int

func init() {
	if runtime.GOMAXPROCS(-1) > 0 && runtime.GOMAXPROCS(-1) < runtime.NumCPU() {
		numCPU = runtime.GOMAXPROCS(-1)
		return
	}
	numCPU = runtime.NumCPU()
}

// Limited creates a Limited pool from the Pool. "size" is the number of goroutines that can execute concurrently.
// If the size is less than 1, it will be set to GOMAXPROCS if that value is less than NumCPU. Otherwise
// NumCPU will be used.
func (p *Pool) Limited(size int) *Limited {
	if size < 1 {
		size = numCPU
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

// PriorityQueue provides a strict priority queue that can be used to submit jobs to the pool.
// This will use the Limited pool to limit the number of concurrent jobs. maxSize
// is the maximum size of the queue. A size < 1 will panic.
// Note: In a PriorityQueue, jobs are processed in order of priority, with higher priority jobs being
// processed first. This means that low priority jobs can stay in the queue forever as long as
// higher priority jobs continue to enter the queue.
func (l *Limited) PriorityQueue(maxSize int) *Queue {
	if maxSize < 1 {
		panic("maxSize must be greater than 0")
	}
	d := &Queue{
		queue: &queue{},
		done:  make(chan struct{}),
		size:  make(chan struct{}, maxSize),
		pool:  l,
	}

	Default().Submit(
		context.Background(),
		d.doWork,
	)
	return d
}
