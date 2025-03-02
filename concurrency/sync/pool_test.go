package sync

import (
	"context"
	"runtime"
	"testing"

	"github.com/tidwall/lotsa"
)

type intType int

func (i *intType) Reset() {
	*i = 0
}

func TestPool(t *testing.T) {
	ctx := context.Background()

	pool := NewPool[*intType](ctx, "", func() *intType { var x intType; return &x }, WithBuffer(10))
	lotsa.Ops(
		1000000,
		runtime.NumCPU(),
		func(i, thread int) {
			x := pool.Get(ctx)
			defer pool.Put(ctx, x)
			if x == nil || *x != 0 {
				panic("bad")
			}
		},
	)
}

func TestCleanup(t *testing.T) {
	ctx := context.Background()

	pool := NewPool[*int](ctx, "", func() *int { var x int; return &x }, WithBuffer(1))

	v := NewCleanup[int](ctx, pool)
	x := v.V()
	*x = 1
	v = nil

	if len(pool.buffer) != 0 {
		t.Fatalf("pool buffer not empty: %d", len(pool.buffer))
	}
	runtime.GC()
	if len(pool.buffer) != 1 {
		t.Fatalf("pool buffer empty after GC: %d", len(pool.buffer))
	}
	buffered := <-pool.buffer
	if *buffered != 1 {
		t.Fatalf("buffered value not 1: %d", *buffered)
	}
}
