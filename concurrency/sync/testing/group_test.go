package sync

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/concurrency/worker"
)

func TestGroupBasic(t *testing.T) {
	t.Parallel()

	pool, err := worker.New(context.Background(), "test")
	if err != nil {
		panic(err)
	}
	defer pool.Close(context.Background())

	limit := pool.Limited(5)

	tests := []struct {
		desc string
		pool sync.WorkerPool
	}{
		{desc: "No pool", pool: nil},
		{desc: "With limited pool", pool: limit},
		{desc: "With pooled pool", pool: pool},
	}

	for _, test := range tests {
		// setup
		var wg = sync.Group{Pool: test.pool}
		var count atomic.Int32
		var exit = make(chan struct{})

		// test go routine
		f := func(ctx context.Context) error {
			count.Add(1)
			defer count.Add(-1)
			<-exit
			return nil
		}

		// spin off 5 go routines
		for i := 0; i < 5; i++ {
			wg.Go(context.Background(), f)
		}

		for count.Load() != 5 {
			time.Sleep(10 * time.Millisecond)
		}

		// check that running count is correct
		if wg.Running() != 5 {
			t.Errorf("TestWaitGroupBasic: Expected Running() to return 5, got %d", wg.Running())
		}
		close(exit)

		// wait for all go routines to finish
		wg.Wait(context.Background())

		// check that running count is 0 after wait
		if wg.Running() != 0 {
			t.Errorf("TestWaitGroupBasic: Expected Running() to return 0, got %d", wg.Running())
		}
	}
}

func TestWaitGroupCancelOnErr(t *testing.T) {
	t.Parallel()

	// setup
	ctx, cancel := context.WithCancel(context.Background())
	wg := sync.Group{CancelOnErr: cancel}

	// spin off 5 go routines
	for i := 0; i < 5; i++ {
		i := i
		wg.Go(
			ctx,
			func(ctx context.Context) error {
				if i == 3 {
					return errors.New("error")
				}
				<-ctx.Done()

				return nil
			},
		)
	}

	if err := wg.Wait(ctx); err == nil {
		t.Errorf("TestWaitGroupCancelOnErr: want error != nil, got nil")
	}
}

func TestIndexErrors(t *testing.T) {
	ctx := context.Background()
	g := sync.Group{}

	for i := 0; i < 5; i++ {
		i := i
		g.Go(
			ctx,
			func(ctx context.Context) error {
				if i%2 == 1 {
					return fmt.Errorf("%d", i)
				}
				return nil
			},
			sync.WithIndex(i),
		)
	}

	err := g.Wait(ctx)
	if err == nil {
		t.Fatalf("TestIndexErrors: want error != nil, got nil")
	}
	expect := map[int]bool{
		1: true,
		3: true,
	}

	for _, entry := range err.(*sync.Errors).Errors() {
		e := entry.(sync.IndexErr)
		if _, ok := expect[e.Index]; !ok {
			t.Fatalf("TestIndexErrors: got unexpected index %d ", e.Index)
		}
		x, err := strconv.Atoi(e.Error())
		if err != nil {
			panic(err)
		}
		if e.Index != x {
			t.Fatalf("TestIndexErrors: got unexpected index %d error %s ", e.Index, e.Error())
		}
		delete(expect, e.Index)
	}
	if len(expect) != 0 {
		t.Fatalf("TestIndexErrors: want no missing indexes, got %v", expect)
	}
}
