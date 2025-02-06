package worker

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestPool(t *testing.T) {
	ctx := context.Background()
	p, err := New(ctx, "myPool")
	if err != nil {
		panic(err)
	}

	verifyPoolGrowth := make(chan bool, 1)
	go func() {
		for {
			if p.goRoutines.Load() > int64(runtime.NumCPU()) {
				verifyPoolGrowth <- true
				return
			}
		}
	}()

	answer := make([]bool, 1000)
	for i := 0; i < 1000; i++ {
		i := i
		p.Submit(
			ctx,
			func() {
				answer[i] = true
				time.Sleep(100 * time.Millisecond)
			},
		)
	}
	p.Wait()

	for i, e := range answer {
		if !e {
			t.Fatalf("TestPool: entry(%d) was not set to true as expected", i)
		}
	}

	// An extra goroutine that isn't used for 1 second is collected by the pool. By waiting two seconds,
	// we should see that the number of goroutines is equal to the number of CPUs.
	time.Sleep(2 * time.Second)
	if p.GoRoutines() != runtime.NumCPU() {
		t.Fatalf("TestPool(number of goroutines): got %d, want %d", p.GoRoutines(), runtime.NumCPU())
	}
	select {
	case <-verifyPoolGrowth:
	default:
		t.Fatalf("TestPool: pool did not grow as expected")
	}
}
