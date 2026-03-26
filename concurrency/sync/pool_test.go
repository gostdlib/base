package sync

import (
	"context"
	"runtime"
	"testing"
	"time"

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

// nonResetter is a concrete type that does NOT implement Resetter.
type nonResetter struct {
	val int
}

func TestPoolPutReset(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name string
		fn   func(t *testing.T, name string)
	}{
		{
			name: "Success: Pool[Resetter] calls Reset on concrete Resetter",
			fn: func(t *testing.T, name string) {
				p := NewPool[Resetter](ctx, "", func() Resetter { var x intType; return &x }, WithBuffer(1))
				v := p.Get(ctx).(*intType)
				*v = 42
				p.Put(ctx, v)
				got := p.Get(ctx).(*intType)
				if *got != 0 {
					t.Errorf("TestPoolPutReset(%s): got %d, want 0", name, *got)
				}
			},
		},
		{
			name: "Success: Pool[any] does not panic for non-Resetter value",
			fn: func(t *testing.T, name string) {
				p := NewPool[any](ctx, "", func() any { return &nonResetter{} }, WithBuffer(1))
				v := p.Get(ctx).(*nonResetter)
				v.val = 99
				p.Put(ctx, v)
				got := p.Get(ctx).(*nonResetter)
				if got.val != 99 {
					t.Errorf("TestPoolPutReset(%s): got val == %d, want 99", name, got.val)
				}
			},
		},
		{
			name: "Success: Pool[any] resets Resetter but leaves non-Resetter unchanged",
			fn: func(t *testing.T, name string) {
				p := NewPool[any](ctx, "", func() any { var x intType; return &x }, WithBuffer(2))
				r := p.Get(ctx).(*intType)
				*r = 10
				p.Put(ctx, r)
				nr := &nonResetter{val: 77}
				p.Put(ctx, nr)

				var sawReset, sawNonResetter bool
				for i := 0; i < 2; i++ {
					v := p.Get(ctx)
					switch x := v.(type) {
					case *intType:
						if *x != 0 {
							t.Errorf("TestPoolPutReset(%s): Resetter value not reset: got %d, want 0", name, *x)
						}
						sawReset = true
					case *nonResetter:
						if x.val != 77 {
							t.Errorf("TestPoolPutReset(%s): nonResetter value changed: got %d, want 77", name, x.val)
						}
						sawNonResetter = true
					}
				}
				if !sawReset {
					t.Errorf("TestPoolPutReset(%s): never saw Resetter value", name)
				}
				if !sawNonResetter {
					t.Errorf("TestPoolPutReset(%s): never saw nonResetter value", name)
				}
			},
		},
	}

	for _, test := range tests {
		test.fn(t, test.name)
	}
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
	time.Sleep(10 * time.Millisecond)
	if len(pool.buffer) != 1 {
		t.Fatalf("pool buffer empty after GC: %d", len(pool.buffer))
	}
	buffered := <-pool.buffer
	if *buffered != 1 {
		t.Fatalf("buffered value not 1: %d", *buffered)
	}
}
