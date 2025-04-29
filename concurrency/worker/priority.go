package worker

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gostdlib/base/concurrency/worker/internal/heap"
)

// QJob represents a job to be done in a priority queue.
type QJob struct {
	// Priority is the job's priority.
	Priority uint64
	// Work is the work to be done by the job.
	Work func()

	// submit is the submit the job was submitted.
	submit time.Time
}

// queue implements the heap interface. We are using a custom generic heap instead of the stdlib.
type queue struct {
	mu   sync.Mutex
	jobs []QJob
}

func (p *queue) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.jobs)
}

func (p *queue) Less(i, j int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Make submission time a tiebreaker.
	if p.jobs[i].Priority == p.jobs[j].Priority {
		return p.jobs[i].submit.Before(p.jobs[j].submit)
	}

	return p.jobs[i].Priority > p.jobs[j].Priority
}

func (p *queue) Swap(i, j int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.jobs[i], p.jobs[j] = p.jobs[j], p.jobs[i]
}

func (p *queue) Push(ctx context.Context, x QJob) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.jobs = append(p.jobs, x)
}

func (p *queue) Pop(ctx context.Context) QJob {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.jobs) == 0 {
		return QJob{}
	}

	n := len(p.jobs) - 1
	job := p.jobs[n]
	p.jobs = p.jobs[0:n]
	return job
}

// Queue represents a priority queue for jobs. This can be created from a Limited Pool via Queue().
// If two jobs have the same priority, the job that was submitted first will be processed first.
type Queue struct {
	queue *queue
	done  chan struct{}
	count atomic.Int64
	wait  sync.WaitGroup
	size  chan struct{}
	pool  *Limited
}

// Close closes the queue. Be sure that the queue is empty before closing.
func (d *Queue) Close() {
	close(d.done)
}

// QueueLen returns the size of the queue not processed. This does not include QJobs that are
// currently being processed.
func (d *Queue) QueueLen() int {
	return int(d.count.Load())
}

// Wait waits for the queue to be empty and not processing being done or the context to be canceled.
// If the context is canceled, the context error will be returned.
func (d *Queue) Wait(ctx context.Context) error {
	done := make(chan struct{})

	Default().Submit(
		ctx,
		func() {
			for {
				if d.QueueLen() == 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			d.wait.Wait()
			close(done)
		},
	)

	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-done:
	}
	return nil
}

// Submit will submit a job to the queue. If the queue is full, it will block until there is room
// in the queue or the context is canceled. A job with priority 0 will be assigned a default priority of 100.
// Valid priority values are 1 - uint64Max. Higher priority jobs (highest being uint64Max) will be
// processed first.
func (d *Queue) Submit(ctx context.Context, job QJob) error {
	if job.Work == nil {
		return errors.New("job has no work")
	}
	if job.Priority == 0 {
		job.Priority = 100
	}
	job.submit = time.Now()

	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case d.size <- struct{}{}:
	}

	heap.Push(ctx, d.queue, job)
	d.count.Add(1)
	return nil
}

// doWork simply sends our QJobs to be done by the worker pool.
func (d *Queue) doWork() {
	for {
		select {
		case <-d.done:
			return
		case <-d.size:
		}

		job := heap.Pop(context.Background(), d.queue)
		d.count.Add(-1)
		if job.Work == nil {
			panic("Bug: job has no work")
		}

		d.wait.Add(1)
		f := func() {
			defer d.wait.Done()
			job.Work()
		}

		if err := d.pool.Submit(context.Background(), f); err != nil {
			log.Printf("error submitting job to pool: %v", err)
		}
	}
}
