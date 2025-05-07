package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gostdlib/base/concurrency/worker/internal/heap"
	"github.com/kylelemons/godebug/pretty"
)

func Test_queue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	q := &queue{popping: make(chan struct{}, 1)}

	// Test Push
	job1 := QJob{Priority: 2, Work: func() {}}
	job2 := QJob{Priority: 3, Work: func() {}}
	job3 := QJob{Priority: 1, Work: func() {}}
	job4 := QJob{Priority: 254, Work: func() {}}

	heap.Push(ctx, q, job1)
	heap.Push(ctx, q, job2)
	if q.Len() != 2 {
		t.Fatalf("Test_queue: got queue length %d, want 2", q.Len())
	}

	// Test Pop: [3, 2]
	poppedJob := heap.Pop(ctx, q)
	if poppedJob.Priority != job2.Priority {
		t.Fatalf("Test_queue: got priority %d, want %d", poppedJob.Priority, job2.Priority)
	}
	if q.Len() != 1 {
		t.Fatalf("Test_queue: got queue length %d, want 1", q.Len())
	}
	heap.Push(ctx, q, job3)
	heap.Push(ctx, q, job4)

	// Test Pop: [254, 2, 1]]
	poppedJob = heap.Pop(ctx, q)
	if poppedJob.Priority != job4.Priority {
		t.Fatalf("Test_queue: got priority %d, want %d", poppedJob.Priority, job4.Priority)
	}
	// Test Pop: [2, 1]
	poppedJob = heap.Pop(ctx, q)
	if poppedJob.Priority != job1.Priority {
		t.Fatalf("Test_queue: got priority %d, want %d", poppedJob.Priority, job1.Priority)
	}
	if q.Len() != 1 {
		t.Fatalf("Test_queue: got queue length %d, want 1", q.Len())
	}
	// Test Pop: [1]
	poppedJob = heap.Pop(ctx, q)
	if poppedJob.Priority != job3.Priority {
		t.Fatalf("Test_queue: got priority %d, want %d", poppedJob.Priority, job3.Priority)
	}
}

func TestQueueSubmitAndProcess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queue := Default().Limited(1).PriorityQueue(10)
	defer queue.Close()

	var processed atomic.Int64

	values := []int{}
	// Submit jobs with varying priorities.
	jobs := []QJob{
		{Priority: 10, Work: func() { time.Sleep(1 * time.Second); processed.Add(1); values = append(values, 10) }},
		{Priority: 50, Work: func() { processed.Add(1); values = append(values, 50) }},
		{Priority: 200, Work: func() { processed.Add(1); values = append(values, 200) }},
		{Priority: 100, Work: func() { processed.Add(1); values = append(values, 100) }},
	}

	for i, job := range jobs {
		if err := queue.Submit(ctx, job); err != nil {
			t.Fatalf("Submit failed: %v", err)
		}
		if i == 0 {
			time.Sleep(10 * time.Millisecond) // Gives time for Priority 10 job to start.
		}
	}

	// Wait for all jobs to be processed.
	if err := queue.Wait(ctx); err != nil {
		t.Fatalf("Queue.Wait() returned an error: %v", err)
	}

	want := []int{10, 200, 100, 50} // 10 is first because we set it up that way.

	if diff := pretty.Compare(want, values); diff != "" {
		t.Errorf("TestQueueSubmitAndProcess: -want/+got:\n%s", diff)
	}
	if processed.Load() != int64(len(jobs)) {
		t.Errorf("Expected %d jobs to be processed, but got %d", len(jobs), processed.Load())
	}
}

func TestQueueLen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queue := Default().Limited(2).PriorityQueue(5)
	defer queue.Close()

	// Submit jobs to the queue.
	for i := 0; i < 3; i++ {
		err := queue.Submit(
			ctx,
			QJob{
				Priority: uint64(i + 1),
				Work:     func() { time.Sleep(100 * time.Millisecond) },
			},
		)
		if err != nil {
			t.Fatalf("Submit failed: %v", err)
		}
	}

	if queue.QueueLen() != 3 {
		t.Errorf("Expected queue length to be 3, but got %d", queue.QueueLen())
	}

	// Wait for all jobs to be processed.
	if err := queue.Wait(ctx); err != nil {
		t.Fatalf("Queue.Wait() returned an error: %v", err)
	}

	if queue.QueueLen() != 0 {
		t.Errorf("Expected queue length to be 0 after processing, but got %d", queue.QueueLen())
	}
}

func TestQueueClose(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := Default().Limited(2)
	queue := pool.PriorityQueue(5)

	err := queue.Submit(
		ctx,
		QJob{
			Priority: 1,
			Work:     func() { time.Sleep(10 * time.Millisecond) },
		},
	)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	queue.Close()

	// After closing, submitting should fail or panic; we test that it doesn't hang.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = queue.Submit(ctx, QJob{
			Priority: 1,
			Work:     func() {},
		})
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Submit after Close() hung unexpectedly")
	}
}
