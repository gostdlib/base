package worker

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestPool(t *testing.T) {
	t.Parallel()

	namedPool, err := New(t.Context(), "myPool")
	if err != nil {
		panic(err)
	}
	runPool(namedPool, t)

	anonPool, err := New(t.Context(), "")
	if err != nil {
		panic(err)
	}
	runPool(anonPool, t)
}

func runPool(p *Pool, t *testing.T) {
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

func TestSubPool(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p, err := New(ctx, "myPool")
	if err != nil {
		panic(err)
	}

	sub1 := p.Sub(ctx, "subPool1")
	sub2 := sub1.Sub(ctx, "subPool2")

	answer := make([]bool, 1000)
	for i := 0; i < 1000; i++ {
		if i%3 == 0 {
			p.Submit(
				ctx,
				func() {
					answer[i] = true
					time.Sleep(100 * time.Millisecond)
				},
			)
		} else if i%3 == 1 {
			sub1.Submit(
				ctx,
				func() {
					answer[i] = true
					time.Sleep(100 * time.Millisecond)
				},
			)
		} else {
			sub2.Submit(
				ctx,
				func() {
					answer[i] = true
					time.Sleep(100 * time.Millisecond)
				},
			)
		}
	}

	p.Wait()
	sub1.Wait()
	sub2.Wait()

	time.Sleep(2 * time.Second) // Wait for extra goroutines to timeout.

	for i, e := range answer {
		if !e {
			t.Fatalf("TestSubPool: entry(%d) was not set to true as expected", i)
		}
	}

	if p.GoRoutines() != sub1.GoRoutines() {
		t.Fatalf("TestSubPool: pool and sub1 did not have the same number of goroutines")
	}

	if p.GoRoutines() != sub2.GoRoutines() {
		t.Fatalf("TestSubPool: pool and sub2 did not have the same number of goroutines")
	}

	if p.StaticPool() != sub1.StaticPool() {
		t.Fatal("TestSubPool: pool and sub1 did not have the same number of static pool goroutines")
	}
	if p.StaticPool() != sub2.StaticPool() {
		t.Fatal("TestSubPool: pool and sub2 did not have the same number of static pool goroutines")
	}

	sub2.Close(ctx)

	at := atomic.Int64{}
	sub1.Submit(
		ctx,
		func() {
			at.Add(1)
		},
	)
	p.Submit(
		ctx,
		func() {
			at.Add(1)
		},
	)
	sub1.Wait()
	p.Wait()
	if at.Load() != 2 {
		t.Fatal("TestSubPool: closing sub2 did something bad")
	}
}
