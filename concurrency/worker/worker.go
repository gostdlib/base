/*
Package worker provides a Pool of goroutines where you can submit Jobs
to be run by an exisiting goroutine instead of spinning off a new goroutine. This pool provides
goroutine reuse in a non-blocking way. The pool will have an always available number of
goroutines equal to the number of CPUs on the machine. Any Submit() calls that cause a queue
block will cause a new goroutine to be created. These extra goroutines will continue to process
jobs until they are idle for a certain amount of time. At that point they will be collected.
This prevents work from being blocked by a queue that is full.

This pool can create other synchronization primitives such as a Limited pool that allows reuse while
limiting the number of concurrent goroutines. You can also create a Group object, an alternative to WaitGroup,
that will allow safe execution of a number of goroutines and then wait for them to finish.

The Pool will also provide metrics on the number of goroutines that have been created and are running.

The Pool is NOT for background processing that has no return values. It is for processing that needs to
be done in the foreground but you don't want to spin off a new goroutine for each job. For background
processing, you should use the base/concurrency/background package.

Normal way of getting a Pool (using the default Pool):

	pool := context.Pool(ctx)

Creating a Pool that has its own metrics:

	pool := context.Pool(ctx).Sub(ctx, "poolNameUniqueToPkg")

Creating a completely separate Pool (rarely needed):

	pool, err := worker.New(ctx, "myPool")
	// Wait for the pool to finish and stop all goroutines. Because we didn't set a deadline
	// this will wait up to 30 seconds. See Close() for details.
	defer p.Close(ctx)

Example of creating and using a Pool:

	ctx := context.Background() // Use base/context package to create a context.

	// Submit a job to the pool.
	// This will be run by an existing goroutine in the pool.
	// If there are no goroutines available, a new one will be created.
	// If the context is canceled before the job is run, the job will not be run.
	// If the context is canceled after the job is run, it is the responsibility of the job to check the context
	// and return if it is canceled.
	err = pool.Submit(
		ctx,
		func() { fmt.Println("Hello, world!") },
	})

Generally you don't wait for the pool to finish. You can just let it run and submit jobs to it. You can call close,
but it is not necessary. If you need to wait for a specific group of goroutines to finish, you can use the
.Group() method.

Example of using the pool for a WaitGroup effect:

	g := pool.Group()

	// Spin off a goroutine that will run a job.
	_ := g.Go(
		ctx,
		func(ctx context.Context) error {
			fmt.Println("Hello, world!")
			return nil
		},
	)

	// Wait for all goroutines to finish. If ctx is canceled, it will return immediately with an error
	// (which we aren't capturing as we aren't cancelling).
	if err := g.Wait(ctx); err != nil {
		// Do something
	}

The above ignores the error in g.Go(), which you only need to look at if supporting Context cancellation.

If you need to limit the number of concurrent goroutines that can run for something, you can create a Limited
pool from the Pool.

Example of creating and using a Limited pool:

	// Create a Limited pool from the Pool.
	// This will limit the number of concurrent goroutines to 10.
	l, err := p.Limited(10)
	if err != nil {
		panic(err)
	}

	l.Submit(
		ctx,
		func() { fmt.Println("Hello, world!") },
	)

	l.Wait() // Again, generally we don't wait for the pool to finish.

You can also use the Limited pool with a WaitGroup effect:

	g := l.WaitGroup()

	g.Go(
		ctx,
		func() error {
			fmt.Println("Hello, world!")
			return nil
		},
	)

	if err := g.Wait(ctx); err != nil {
		// Do something
	}

We also provide a PriorityQueue for running jobs from Limited pools.

	for i, work := range []func() {
		job := QJob{Priority: i, Work: work}
		limitedPool.Submit(ctx, job)
	}

This package also offers a Promise object for submitting a job and getting the result back. This is useful for
when you want to run a job, do some other work, and then get the result back.

	p := NewPromise[string]()

	_ := g.Go(
		ctx,
		func() error {
			p.Set(ctx, "Hello, world!", nil)
			return nil
		},
	)

	fmt.Println(p.Get().V)
*/
package worker

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	bSync "github.com/gostdlib/base/concurrency/sync"
	internalCtx "github.com/gostdlib/base/internal/context"
	"github.com/gostdlib/base/telemetry/otel/metrics"
	"github.com/gostdlib/base/telemetry/otel/trace/span"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Pool provides a worker pool that can be used to submit functions to be run by a goroutine. This provides
// goroutine reuse in a non-blocking way. The pool will have an always available number of goroutines equal
// to the number of CPUs on the machine. Any Submit() calls that exceed this number will cause a new goroutine
// to be created that will run until some idle timeout. You can create other synchronization primitives
// such as a Limited pool that allows reuse while limiting the number of concurrent goroutines. You can also
// create a Group object that will allow safe execution of a number of goroutines and then wait for them to
// finish. Generally you only need to use a single Pool for an entire application. If using the
// base/context package, there is a Pool tied to the context that can be used.
type Pool struct {
	// queue is the channel that contains the functions to be run, populated by Submit().
	queue chan runArgs
	opts  poolOpts

	wg         sync.WaitGroup
	running    atomic.Int64
	goRoutines atomic.Int64
	metrics    *poolMetrics

	// child indicates this pool is not a root pool but one that is created with .Sub().
	child bool
}

type poolOpts struct {
	// size is the number of always available goroutines.
	size int
	// timeout is the time to wait for a job before timing out. If a timeout occurs the goroutine will be
	// collected.
	timeout time.Duration
}

func (p poolOpts) defaults() poolOpts {
	if p.size < 1 {
		p.size = runtime.NumCPU()
	}
	if p.timeout <= 0 {
		p.timeout = time.Second
	}
	return p
}

// Option is an option for New().
type Option func(poolOpts) (poolOpts, error)

// WithSize sets the amount of goroutines that are always available. By default this is set to the number
// of CPUs on the machine. Any submissions that exceed this number will cause a new goroutine to be created
// and stored in a sync.Pool for reuse.For spikey workloads, the defaults should be sufficient. For constant
// high loads, you may want to increase this number. Remember that increased number of goroutines over the
// number of CPUs will cause context switching and slow down processing if doing data intensive work that doesn't
// require immediate responses.
func WithSize(size int) func(poolOpts) (poolOpts, error) {
	return func(opts poolOpts) (poolOpts, error) {
		if size < 1 {
			return opts, fmt.Errorf("cannot have a Pool with size < 1")
		}
		opts.size = size
		return opts, nil
	}
}

// WithRunnerTimeout sets the time a goroutine runner that is not always available will wait for a job before
// timing out. If a timeout occurs, the goroutine will be collected. If <= 0, there is no timeout which means
// that any new goroutine created will always be available and never be collected. That is usually not a good
// idea that can cause memory leaks via goroutine leaks. Default is 1 second.
func WithRunnerTimeout(timeout time.Duration) func(poolOpts) (poolOpts, error) {
	return func(opts poolOpts) (poolOpts, error) {
		opts.timeout = timeout
		return opts, nil
	}
}

// New creates a new worker pool. The name is used for logging and metrics. The pool will have an always
// available number of goroutines equal to the number of CPUs on the machine. Any Submit() calls that exceed
// this number will cause a new goroutine to be created. The context should
// have the meter provider via our context package to allow for metrics to be emitted.
func New(ctx context.Context, name string, options ...Option) (*Pool, error) {
	opts := poolOpts{}.defaults()
	for _, o := range options {
		var err error
		opts, err = o(opts)
		if err != nil {
			return nil, err
		}
	}

	queue := make(chan runArgs, 1)

	mp := internalCtx.MeterProvider(ctx)
	meter := mp.Meter(metrics.MeterName(2) + "/" + name)
	pm := newPoolMetrics(meter)

	p := &Pool{
		queue:   queue,
		opts:    opts,
		metrics: pm,
	}

	// Start the goroutines that will run forever.
	for i := 0; i < opts.size; i++ {
		r := runner{queue: queue, goRoutines: &p.goRoutines, metrics: pm}
		go r.run()
	}

	return p, nil
}

// Close waits for all submitted jobs to stop, then stops all goroutines. It is ALMOST ALWAYS A BAD IDEA
// TO USE THIS. Almost always using this is using a bad pattern. To use this safely you should not use
// .Sub(), .Group(), ... that can use this pool, because then you don't have control of tasks that can take too
// long. The best use case is to set a default pool that Context uses and then make subpools, limited pools and
// groups from this one pool. This gives maximum performance and resource control. And this can live until the
// program dies.
//
// If you really need Close(), it will wait until the passed Context deadline for everything to stop. If the
// deadline is not set, this has a maximum wait time of 30 * time.Second. If the pool is not closed by then,
// it will return. If you need to wait for all jobs to finish no matter how long it takes,
// use Wait() then call Close(). However, this can lead to a deadlock if you are waiting for a job that
// never finishes. If ctx is cancelled, Close will return immediately with the results of context.Cause(ctx).
// Closing a Sub pool will not have any effect on the parent.
func (p *Pool) Close(ctx context.Context) error {
	done := make(chan struct{})

	go func() {
		p.wg.Wait() // Wait for execution to finish.
		if !p.child {
			close(p.queue) // Kill all goroutines.
		}
		close(done) // Inform this function that we are done.
	}()

	var timer *time.Timer
	if deadline, ok := ctx.Deadline(); ok {
		timer = time.NewTimer(time.Until(deadline))
	} else {
		timer = time.NewTimer(30 * time.Second)
	}
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return fmt.Errorf("timed out waiting for pool to close")
	case <-done:
		return nil
	}
}

// Wait will wait for all goroutines in the pool to finish execution. The pool's goroutines will continue to
// run and be available for reuse. Rarely used. Generally you should use .Group() to wait for a specific group of
// goroutines to finish. If a Sub pool, this will only wait for its running goroutines, not the parent. The parent
// also won't wait for the children.
func (p *Pool) Wait() {
	p.wg.Wait()
}

// Len returns the current entries in the the channel that distributes to the pool.
func (p *Pool) Len() int {
	return len(p.queue)
}

// Running returns the number of running jobs in the pool. If this is a sub pool, it will only be the
// number for that pool
func (p *Pool) Running() int {
	return int(p.running.Load())
}

// GoRoutines returns the total number of goroutines that are currently in the pool. If this is Sub pool,
// this will be the number in the parent pool.
func (p *Pool) GoRoutines() int {
	return int(p.goRoutines.Load())
}

// StaticPool is the number of goroutines in the static pool in the pool or if a Sub pool, the parent pool.
func (p *Pool) StaticPool() int {
	return int(p.opts.size)
}

// Submit submits the function to be executed. If the context is canceled before the
// function is executed, the function will not be executed. Once the function is executed,
// it is the responsibility of the function to check the context and return if it is canceled.
func (p *Pool) Submit(ctx context.Context, f func()) error {
	spanner := span.Get(ctx)

	if f == nil {
		err := fmt.Errorf("worker.Pool: cannot submit a runner that is nil")
		spanner.Span.RecordError(err)
		return err
	}

	now := time.Now()

	args := p.newRunArgs(f)

	p.wg.Add(1)
	select {
	// User cancelled before we could submit.
	case <-ctx.Done():
		args.done() // This will decrement the waitgroup.
		return context.Cause(ctx)
	// Try to submit the job.
	case p.queue <- args:
	// We couldn't submit the job because the queue is full. We will create a new goroutine
	// and submit the job to that goroutine. This goroutine will be collected if it sits idle.
	// for too long.
	default:
	tryAgain:
		r := runner{queue: p.queue, timeout: p.opts.timeout, goRoutines: &p.goRoutines, metrics: p.metrics}
		go r.run()

		select {
		case <-ctx.Done():
			args.done() // This will decrement the waitgroup.
			return context.Cause(ctx)
		case p.queue <- args:
		// default can happen if the queue fills again with another job before we can submit. In those cases,
		// we will try again to create a new goroutine and submit the job. This is a rare case, but can happen
		// if the number of CPUs is very low.
		default:
			goto tryAgain
		}
	}
	p.submitEvent(spanner, now)
	return nil
}

// Group returns a sync.Group that can be used to spin off goroutines and then wait for them to finish.
// This will use the Pool. Safer than a sync.Group.
func (p *Pool) Group() bSync.Group {
	return bSync.Group{Pool: p}
}

// Sub is used to create a new Pool that is backed by the current pool. This allows having
// shared pools that record different metrics.
func (p *Pool) Sub(ctx context.Context, name string) *Pool {
	mp := internalCtx.MeterProvider(ctx)
	meter := mp.Meter(metrics.MeterName(2) + "/" + name)
	pm := newPoolMetrics(meter)

	// Even though we are backed by a pool that might have more goroutines, this has its own size.
	var goRoutines int64
	if p.opts.size > 0 {
		goRoutines = int64(p.opts.size)
	} else {
		goRoutines = int64(runtime.NumCPU())
	}

	pool := &Pool{
		queue:   p.queue,
		opts:    p.opts,
		metrics: pm,
		child:   true,
	}
	pool.goRoutines.Add(goRoutines)
	return pool
}

// Meter returns the meter for this pool.
func (p *Pool) Meter() metric.Meter {
	if p.metrics == nil {
		return nil
	}
	return p.metrics.meter
}

func (p *Pool) submitEvent(spanner span.Span, t time.Time) {
	spanner.Event(
		"Pool.Submit()",
		attribute.Int64("submit_latency_ns", int64(time.Since(t))),
	)
}

// runArgs is the arguments for a job to be run.
type runArgs struct {
	// f is the function to be run.
	f func()
	// p is the pool that the job is being run in.
	p *Pool
}

// run runs the function.
func (r runArgs) run() {
	r.p.running.Add(1)
	r.f()
	r.done()
	r.p.running.Add(-1)
}

// done is called when the job is done.
func (r runArgs) done() {
	// This happens regardless if onDone is nil or not, as this waitgroup is for
	// the pool to know when the job is done.
	defer r.p.wg.Done()
}

// newRunArgs creates a new runArgs that will run f() and call onDone() when done. onDone can be nil.
func (p *Pool) newRunArgs(f func()) runArgs {
	return runArgs{f: f, p: p}
}

// runner is type used to listen for requests to run functions and then execute them one at at time.
// If the queue is empty, the runner will wait for a job to be submitted. If timeout is set, then the
// runner will wait for a job for that amount of time before timing out and being collected. This allows us
// to have a pool of goroutines that are always available and a pool of goroutines that are created on demand
// and then collected if they are idle for too long.
type runner struct {
	// goRoutines is the number of goroutines that are currently running. This is passed
	// by reference from the Pool to keep track of the number of running goroutines.
	goRoutines *atomic.Int64
	// queue is the channel that contains the functions to be run.
	queue chan runArgs
	// timeout is the time to wait for a job before timing out. If <= 0, there is no timeout which means
	// the runner will always be available and never be collected.
	timeout time.Duration

	metrics *poolMetrics
}

// run is the main loop for the runner. It will wait for a job to be submitted and then run it. If there is a
// timeout, it will wait for that amount of time before timing out and being collected. This should
// be run in a goroutine.
func (r runner) run() {
	var t *time.Timer
	if r.timeout > 0 {
		t = time.NewTimer(r.timeout)
		r.metrics.StaticExists.Add(context.Background(), 1)
		defer r.metrics.StaticExists.Add(context.Background(), -1)
	} else {
		r.metrics.DynamicExists.Add(context.Background(), 1)
		defer r.metrics.DynamicExists.Add(context.Background(), -1)
		r.metrics.DynamicTotal.Add(context.Background(), 1)
	}
	r.goRoutines.Add(1)
	defer r.goRoutines.Add(-1)
	for {
		if r.timeout > 0 {
			if err := r.runTimer(t); err != nil {
				return
			}
			continue
		}
		if err := r.runAlways(); err != nil {
			return
		}
	}
}

// runAlways runs the runner without a timeout. However it can be stopped by closing the queue.
func (r runner) runAlways() error {
	args, ok := <-r.queue
	if !ok {
		return fmt.Errorf("runner canceled")
	}
	r.metrics.StaticRunning.Add(context.Background(), 1)
	args.run()
	r.metrics.StaticRunning.Add(context.Background(), -1)
	return nil
}

// runTimer runs the runner with a timeout. If the timeout is reached, the runner will be collected.
func (r runner) runTimer(t *time.Timer) error {
	t.Reset(r.timeout)
	defer t.Stop()

	select {
	case args, ok := <-r.queue:
		if !ok {
			return fmt.Errorf("runner canceled")
		}
		r.metrics.DynamicRunning.Add(context.Background(), 1)
		args.run()
		r.metrics.DynamicRunning.Add(context.Background(), -1)
		return nil
	case <-t.C:
		return fmt.Errorf("runner timed out")
	}
}
