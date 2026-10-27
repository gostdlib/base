package sync

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/kylelemons/godebug/pretty"
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

// noResetBuf is a buffer-shaped value type without Reset(). Converting it to an interface would copy its slice header to
// the heap, so a Put that boxed it would allocate.
type noResetBuf struct {
	b []byte
}

func TestPoolReset(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	tests := []struct {
		name string
		fn   func(t *testing.T, name string)
	}{
		{
			name: "Success: a value type with a pointer receiver Reset is reset",
			fn: func(t *testing.T, name string) {
				p := NewPool(ctx, "", func() valueResetter { return valueResetter{} }, WithBuffer(1))
				p.Put(ctx, valueResetter{n: 5})
				if diff := pretty.Compare(valueResetter{}, p.Get(ctx)); diff != "" {
					t.Errorf("TestPoolReset(%s): -want/+got:\n%s", name, diff)
				}
			},
		},
		{
			name: "Success: a value type with a value receiver Reset is reset",
			fn: func(t *testing.T, name string) {
				p := NewPool(ctx, "", func() valueReceiverResetter { return valueReceiverResetter{n: new(int)} }, WithBuffer(1))
				n := 5
				p.Put(ctx, valueReceiverResetter{n: &n})
				p.Get(ctx)
				if n != 0 {
					t.Errorf("TestPoolReset(%s): got n == %d, want 0", name, n)
				}
			},
		},
		{
			name: "Success: a pointer type with Reset is reset",
			fn: func(t *testing.T, name string) {
				p := NewPool(ctx, "", func() *intType { var x intType; return &x }, WithBuffer(1))
				x := intType(5)
				p.Put(ctx, &x)
				if got := p.Get(ctx); *got != 0 {
					t.Errorf("TestPoolReset(%s): got %d, want 0", name, *got)
				}
			},
		},
		{
			name: "Success: a value type without Reset is put back unchanged",
			fn: func(t *testing.T, name string) {
				p := NewPool(ctx, "", func() nonResetter { return nonResetter{} }, WithBuffer(1))
				p.Put(ctx, nonResetter{val: 5})
				if diff := pretty.Compare(nonResetter{val: 5}, p.Get(ctx)); diff != "" {
					t.Errorf("TestPoolReset(%s): -want/+got:\n%s", name, diff)
				}
			},
		},
		{
			name: "Success: Pool[Resetter] calls Reset on concrete Resetter",
			fn: func(t *testing.T, name string) {
				p := NewPool[Resetter](ctx, "", func() Resetter { var x intType; return &x }, WithBuffer(1))
				v := p.Get(ctx).(*intType)
				*v = 42
				p.Put(ctx, v)
				got := p.Get(ctx).(*intType)
				if *got != 0 {
					t.Errorf("TestPoolReset(%s): got %d, want 0", name, *got)
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
					t.Errorf("TestPoolReset(%s): got val == %d, want 99", name, got.val)
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
							t.Errorf("TestPoolReset(%s): Resetter value not reset: got %d, want 0", name, *x)
						}
						sawReset = true
					case *nonResetter:
						if x.val != 77 {
							t.Errorf("TestPoolReset(%s): nonResetter value changed: got %d, want 77", name, x.val)
						}
						sawNonResetter = true
					}
				}
				if !sawReset {
					t.Errorf("TestPoolReset(%s): never saw Resetter value", name)
				}
				if !sawNonResetter {
					t.Errorf("TestPoolReset(%s): never saw nonResetter value", name)
				}
			},
		},
	}

	for _, test := range tests {
		test.fn(t, test.name)
	}
}

// getPutAllocs returns the allocations of one Get and Put on a buffered Pool of T. The buffer makes every Get a hit, so
// the count is what Put costs, including any Reset().
func getPutAllocs[T any](ctx context.Context, n func() T) float64 {
	p := NewPool(ctx, "", n, WithBuffer(1))
	p.Put(ctx, n())
	return testing.AllocsPerRun(1000, func() {
		p.Put(ctx, p.Get(ctx))
	})
}

// TestPoolPutAllocs pins what a Put costs for each kind of T: only a value type whose Reset() has a pointer receiver
// allocates, because Reset needs the address of a copy that then escapes to the heap.
func TestPoolPutAllocs(t *testing.T) {
	// Sequential: it measures allocations.
	if raceEnabled {
		t.Skip("the race detector adds allocations, so the counts do not hold")
	}
	ctx := t.Context()

	tests := []struct {
		name   string
		allocs func() float64
		want   float64
	}{
		{
			name: "Success: a value type without Reset allocates nothing",
			allocs: func() float64 {
				return getPutAllocs(ctx, func() noResetBuf { return noResetBuf{b: make([]byte, 0, 64)} })
			},
			want: 0,
		},
		{
			name:   "Success: a pointer type with Reset allocates nothing",
			allocs: func() float64 { return getPutAllocs(ctx, func() *intType { var x intType; return &x }) },
			want:   0,
		},
		{
			name:   "Success: an interface type holding a Resetter allocates nothing",
			allocs: func() float64 { return getPutAllocs(ctx, func() Resetter { var x intType; return &x }) },
			want:   0,
		},
		{
			name:   "Success: an interface type holding a value without Reset allocates nothing",
			allocs: func() float64 { return getPutAllocs(ctx, func() any { return &nonResetter{} }) },
			want:   0,
		},
		{
			name: "Success: a pointer-shaped value type with a value receiver Reset allocates nothing",
			allocs: func() float64 {
				return getPutAllocs(ctx, func() valueReceiverResetter { return valueReceiverResetter{n: new(int)} })
			},
			want: 0,
		},
		{
			name: "Success: a value type with a pointer receiver Reset allocates once",
			allocs: func() float64 {
				return getPutAllocs(ctx, func() sliceResetter { return sliceResetter{b: make([]byte, 0, 64)} })
			},
			want: 1,
		},
	}

	for _, test := range tests {
		if got := test.allocs(); got != test.want {
			t.Errorf("TestPoolPutAllocs(%s): got %v allocs per Get+Put, want %v", test.name, got, test.want)
		}
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
