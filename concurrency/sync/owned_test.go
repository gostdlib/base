package sync

import (
	"context"
	"sync/atomic"
	"testing"
)

// valueResetter has a pointer receiver Reset(), so a valueResetter value does not satisfy Resetter but &v does.
type valueResetter struct {
	n int
}

func (v *valueResetter) Reset() {
	v.n = 0
}

func newIntOwnedPool(ctx context.Context) *OwnedPool[*intType] {
	return NewOwnedPool(ctx, "", func() *intType { var x intType; return &x })
}

// panics reports if f panics.
func panics(f func()) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	f()
	return false
}

func TestNextOwner(t *testing.T) {
	t.Parallel()

	a, b := NextOwner(), NextOwner()
	if a == 0 || b == 0 {
		t.Errorf("TestNextOwner: got owners %d and %d, want both != 0", a, b)
	}
	if a == b {
		t.Errorf("TestNextOwner: got owner %d twice, want unique owners", a)
	}
}

func TestOwnedPool(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	tests := []struct {
		name      string
		fn        func(t *testing.T)
		wantPanic bool
	}{
		{
			name: "Success: Get, V and Put by the owner, pointer Resetter is reset",
			fn: func(t *testing.T) {
				p := NewOwnedPool(ctx, "", func() *intType { var x intType; return &x }, WithBuffer(1))
				o := NextOwner()
				v := p.Get(ctx, o)
				*v.V(o) = 42
				p.Put(ctx, o, v)

				o2 := NextOwner()
				got := p.Get(ctx, o2)
				if *got.V(o2) != 0 {
					t.Errorf("TestOwnedPool: got %d, want 0", *got.V(o2))
				}
			},
		},
		{
			name: "Success: value type with pointer receiver Reset is reset",
			fn: func(t *testing.T) {
				p := NewOwnedPool(ctx, "", func() valueResetter { return valueResetter{n: 1} }, WithBuffer(1))
				o := NextOwner()
				v := p.Get(ctx, o)
				if v.V(o).n != 1 {
					t.Errorf("TestOwnedPool: got n == %d, want 1", v.V(o).n)
				}
				p.Put(ctx, o, v)

				o2 := NextOwner()
				got := p.Get(ctx, o2)
				if got.V(o2).n != 0 {
					t.Errorf("TestOwnedPool: got n == %d, want 0", got.V(o2).n)
				}
			},
		},
		{
			name: "Success: interface type whose value implements Resetter is reset",
			fn: func(t *testing.T) {
				p := NewOwnedPool(ctx, "", func() Resetter { var x intType; return &x }, WithBuffer(1))
				o := NextOwner()
				v := p.Get(ctx, o)
				*v.V(o).(*intType) = 42
				p.Put(ctx, o, v)

				o2 := NextOwner()
				got := p.Get(ctx, o2)
				if *got.V(o2).(*intType) != 0 {
					t.Errorf("TestOwnedPool: got %d, want 0", *got.V(o2).(*intType))
				}
			},
		},
		{
			name: "Success: value type without Reset is put back unchanged",
			fn: func(t *testing.T) {
				p := NewOwnedPool(ctx, "", func() int { return 0 }, WithBuffer(1))
				o := NextOwner()
				v := p.Get(ctx, o)
				*v.Ptr(o) = 5
				p.Put(ctx, o, v)

				o2 := NextOwner()
				got := p.Get(ctx, o2)
				if *got.Ptr(o2) != 5 {
					t.Errorf("TestOwnedPool: got %d, want 5", *got.Ptr(o2))
				}
			},
		},
		{
			name: "Success: a value put into a second pool of the same type is reset there",
			fn: func(t *testing.T) {
				first := NewOwnedPool(ctx, "", func() valueResetter { return valueResetter{} })
				second := NewOwnedPool(ctx, "", func() valueResetter { return valueResetter{} }, WithBuffer(1))
				o := NextOwner()
				v := first.Get(ctx, o)
				v.Ptr(o).n = 9
				second.Put(ctx, o, v)

				o2 := NextOwner()
				got := second.Get(ctx, o2)
				if got.V(o2).n != 0 {
					t.Errorf("TestOwnedPool: got n == %d, want 0", got.V(o2).n)
				}
			},
		},
		{
			name: "Success: Validate by the owner does not panic",
			fn: func(t *testing.T) {
				o := NextOwner()
				newIntOwnedPool(ctx).Get(ctx, o).Validate(o)
			},
		},
		{
			name: "Success: Transfer lets the new owner use V and Put",
			fn: func(t *testing.T) {
				p := newIntOwnedPool(ctx)
				o, o2 := NextOwner(), NextOwner()
				v := p.Get(ctx, o)
				*v.V(o) = 7
				v.Transfer(o, o2)
				if *v.V(o2) != 7 {
					t.Errorf("TestOwnedPool: got %d, want 7", *v.V(o2))
				}
				p.Put(ctx, o2, v)
			},
		},
		{
			name: "Success: Ptr lets the owner change a value type in place, and Put resets it",
			fn: func(t *testing.T) {
				p := NewOwnedPool(ctx, "", func() valueResetter { return valueResetter{} }, WithBuffer(1))
				o := NextOwner()
				v := p.Get(ctx, o)
				v.Ptr(o).n = 5
				if v.V(o).n != 5 {
					t.Errorf("TestOwnedPool: got n == %d, want 5", v.V(o).n)
				}
				p.Put(ctx, o, v)

				o2 := NextOwner()
				got := p.Get(ctx, o2)
				if got.Ptr(o2).n != 0 {
					t.Errorf("TestOwnedPool: got n == %d, want 0", got.Ptr(o2).n)
				}
			},
		},
		{
			name: "Success: Ptr by the new owner after Transfer",
			fn: func(t *testing.T) {
				p := NewOwnedPool(ctx, "", func() valueResetter { return valueResetter{} }, WithBuffer(1))
				o, o2 := NextOwner(), NextOwner()
				v := p.Get(ctx, o)
				v.Ptr(o).n = 3
				v.Transfer(o, o2)
				if v.Ptr(o2).n != 3 {
					t.Errorf("TestOwnedPool: got n == %d, want 3", v.Ptr(o2).n)
				}
			},
		},
		{
			name: "Error: Ptr by a non-owner panics",
			fn: func(t *testing.T) {
				v := newIntOwnedPool(ctx).Get(ctx, NextOwner())
				v.Ptr(NextOwner())
			},
			wantPanic: true,
		},
		{
			name: "Error: Ptr by the old owner after Put panics",
			fn: func(t *testing.T) {
				p := newIntOwnedPool(ctx)
				o := NextOwner()
				v := p.Get(ctx, o)
				p.Put(ctx, o, v)
				v.Ptr(o)
			},
			wantPanic: true,
		},
		{
			name: "Error: Ptr by the old owner after Transfer panics",
			fn: func(t *testing.T) {
				p := newIntOwnedPool(ctx)
				o := NextOwner()
				v := p.Get(ctx, o)
				v.Transfer(o, NextOwner())
				v.Ptr(o)
			},
			wantPanic: true,
		},
		{
			name: "Error: Get with owner 0 panics",
			fn: func(t *testing.T) {
				newIntOwnedPool(ctx).Get(ctx, 0)
			},
			wantPanic: true,
		},
		{
			name: "Error: V by a non-owner panics",
			fn: func(t *testing.T) {
				v := newIntOwnedPool(ctx).Get(ctx, NextOwner())
				v.V(NextOwner())
			},
			wantPanic: true,
		},
		{
			name: "Error: Validate by a non-owner panics",
			fn: func(t *testing.T) {
				v := newIntOwnedPool(ctx).Get(ctx, NextOwner())
				v.Validate(NextOwner())
			},
			wantPanic: true,
		},
		{
			name: "Error: V by the old owner after Put panics",
			fn: func(t *testing.T) {
				p := newIntOwnedPool(ctx)
				o := NextOwner()
				v := p.Get(ctx, o)
				p.Put(ctx, o, v)
				v.V(o)
			},
			wantPanic: true,
		},
		{
			name: "Error: V with owner 0 after Put panics",
			fn: func(t *testing.T) {
				p := newIntOwnedPool(ctx)
				o := NextOwner()
				v := p.Get(ctx, o)
				p.Put(ctx, o, v)
				v.V(0)
			},
			wantPanic: true,
		},
		{
			name: "Error: Put by a non-owner panics",
			fn: func(t *testing.T) {
				p := newIntOwnedPool(ctx)
				v := p.Get(ctx, NextOwner())
				p.Put(ctx, NextOwner(), v)
			},
			wantPanic: true,
		},
		{
			name: "Error: double Put by the owner panics",
			fn: func(t *testing.T) {
				p := newIntOwnedPool(ctx)
				o := NextOwner()
				v := p.Get(ctx, o)
				p.Put(ctx, o, v)
				p.Put(ctx, o, v)
			},
			wantPanic: true,
		},
		{
			name: "Error: Put with owner 0 after Put panics",
			fn: func(t *testing.T) {
				p := newIntOwnedPool(ctx)
				o := NextOwner()
				v := p.Get(ctx, o)
				p.Put(ctx, o, v)
				p.Put(ctx, 0, v)
			},
			wantPanic: true,
		},
		{
			name: "Error: Transfer by a non-owner panics",
			fn: func(t *testing.T) {
				v := newIntOwnedPool(ctx).Get(ctx, NextOwner())
				v.Transfer(NextOwner(), NextOwner())
			},
			wantPanic: true,
		},
		{
			name: "Error: Transfer to owner 0 panics",
			fn: func(t *testing.T) {
				o := NextOwner()
				v := newIntOwnedPool(ctx).Get(ctx, o)
				v.Transfer(o, 0)
			},
			wantPanic: true,
		},
		{
			name: "Error: Transfer from owner 0 after Put panics",
			fn: func(t *testing.T) {
				p := newIntOwnedPool(ctx)
				o := NextOwner()
				v := p.Get(ctx, o)
				p.Put(ctx, o, v)
				v.Transfer(0, NextOwner())
			},
			wantPanic: true,
		},
		{
			name: "Error: V by the old owner after Transfer panics",
			fn: func(t *testing.T) {
				o := NextOwner()
				v := newIntOwnedPool(ctx).Get(ctx, o)
				v.Transfer(o, NextOwner())
				v.V(o)
			},
			wantPanic: true,
		},
		{
			name: "Error: Put by the old owner after Transfer panics",
			fn: func(t *testing.T) {
				p := newIntOwnedPool(ctx)
				o := NextOwner()
				v := p.Get(ctx, o)
				v.Transfer(o, NextOwner())
				p.Put(ctx, o, v)
			},
			wantPanic: true,
		},
	}

	for _, test := range tests {
		panicked := panics(func() { test.fn(t) })

		switch {
		case panicked && !test.wantPanic:
			t.Errorf("TestOwnedPool(%s): got panic, want no panic", test.name)
		case !panicked && test.wantPanic:
			t.Errorf("TestOwnedPool(%s): got no panic, want panic", test.name)
		}
	}
}

// TestOwnedConcurrentRelease races two calls that both release ownership of the same value from the same owner,
// as happens when two goroutines wrongly believe they own it. Exactly one must win and the other must panic.
func TestOwnedConcurrentRelease(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	const iterations = 1000

	tests := []struct {
		name string
		a, b func(p *OwnedPool[*intType], o uint64, v *Owned[*intType])
	}{
		{
			name: "Success: concurrent Put and Put, exactly one panics",
			a:    func(p *OwnedPool[*intType], o uint64, v *Owned[*intType]) { p.Put(ctx, o, v) },
			b:    func(p *OwnedPool[*intType], o uint64, v *Owned[*intType]) { p.Put(ctx, o, v) },
		},
		{
			name: "Success: concurrent Transfer and Transfer, exactly one panics",
			a:    func(p *OwnedPool[*intType], o uint64, v *Owned[*intType]) { v.Transfer(o, NextOwner()) },
			b:    func(p *OwnedPool[*intType], o uint64, v *Owned[*intType]) { v.Transfer(o, NextOwner()) },
		},
		{
			name: "Success: concurrent Put and Transfer, exactly one panics",
			a:    func(p *OwnedPool[*intType], o uint64, v *Owned[*intType]) { p.Put(ctx, o, v) },
			b:    func(p *OwnedPool[*intType], o uint64, v *Owned[*intType]) { v.Transfer(o, NextOwner()) },
		},
	}

	for _, test := range tests {
		p := newIntOwnedPool(ctx)
		for i := 0; i < iterations; i++ {
			o := NextOwner()
			v := p.Get(ctx, o)

			var panicCount atomic.Int32
			g := Group{}
			for _, op := range []func(*OwnedPool[*intType], uint64, *Owned[*intType]){test.a, test.b} {
				g.Go(
					ctx,
					func(ctx context.Context) error {
						if panics(func() { op(p, o, v) }) {
							panicCount.Add(1)
						}
						return nil
					},
				)
			}
			if err := g.Wait(ctx); err != nil {
				t.Fatalf("TestOwnedConcurrentRelease(%s): got err == %s, want err == nil", test.name, err)
			}
			if got := panicCount.Load(); got != 1 {
				t.Errorf("TestOwnedConcurrentRelease(%s): iteration %d: got %d panics, want 1", test.name, i, got)
				break
			}
		}
	}
}

// TestOwnedPoolValueTypeMissAllocs pins why Ptr exists: a pool of a value type stores the value inside the Owned, so a
// pool miss allocates once, where a pointer type allocates the Owned and the value separately.
func TestOwnedPoolValueTypeMissAllocs(t *testing.T) {
	// Sequential: it measures allocations.
	ctx := t.Context()

	values := NewOwnedPool(ctx, "", func() valueResetter { return valueResetter{} })
	pointers := NewOwnedPool(ctx, "", func() *valueResetter { return &valueResetter{} })

	// Every Get is a miss: nothing is ever Put back.
	gotValues := testing.AllocsPerRun(100, func() {
		o := NextOwner()
		values.Get(ctx, o).Ptr(o).n = 1
	})
	gotPointers := testing.AllocsPerRun(100, func() {
		o := NextOwner()
		pointers.Get(ctx, o).V(o).n = 1
	})

	if gotValues >= gotPointers {
		t.Errorf("TestOwnedPoolValueTypeMissAllocs: got %v allocs per value-type miss, want fewer than the %v of a pointer type", gotValues, gotPointers)
	}
}

// valueReceiverResetter has a value receiver Reset(), so both valueReceiverResetter and its pointer satisfy Resetter.
// Reset clears through a pointer field, because a value receiver cannot change the value itself.
type valueReceiverResetter struct {
	n *int
}

func (v valueReceiverResetter) Reset() {
	*v.n = 0
}

func TestResetFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// run builds an Owned holding a value whose state is 5, resets it with the function resetFor picks (when it
		// picks one) and returns the state afterwards and whether a function was picked.
		run       func() (state int, picked bool)
		wantState int
		wantNil   bool
	}{
		{
			name: "Success: a value type with a pointer receiver Reset is reset",
			run: func() (int, bool) {
				o := &Owned[valueResetter]{v: valueResetter{n: 5}}
				return applyReset(resetFor[valueResetter](), o, func() int { return o.v.n })
			},
			wantState: 0,
		},
		{
			name: "Success: a value type with a value receiver Reset is reset",
			run: func() (int, bool) {
				n := 5
				o := &Owned[valueReceiverResetter]{v: valueReceiverResetter{n: &n}}
				return applyReset(resetFor[valueReceiverResetter](), o, func() int { return n })
			},
			wantState: 0,
		},
		{
			name: "Success: a pointer type with Reset is reset",
			run: func() (int, bool) {
				x := intType(5)
				o := &Owned[*intType]{v: &x}
				return applyReset(resetFor[*intType](), o, func() int { return int(x) })
			},
			wantState: 0,
		},
		{
			name: "Success: an interface type holding a Resetter is reset",
			run: func() (int, bool) {
				x := intType(5)
				o := &Owned[Resetter]{v: &x}
				return applyReset(resetFor[Resetter](), o, func() int { return int(x) })
			},
			wantState: 0,
		},
		{
			name: "Success: an interface type holding a value without Reset is left unchanged",
			run: func() (int, bool) {
				o := &Owned[any]{v: 5}
				return applyReset(resetFor[any](), o, func() int { return o.v.(int) })
			},
			wantState: 5,
		},
		{
			name: "Success: a type without Reset gets no reset function",
			run: func() (int, bool) {
				o := &Owned[int]{v: 5}
				return applyReset(resetFor[int](), o, func() int { return o.v })
			},
			wantState: 5,
			wantNil:   true,
		},
	}

	for _, test := range tests {
		state, picked := test.run()
		if picked == test.wantNil {
			t.Errorf("TestResetFor(%s): got a reset function == %v, want %v", test.name, picked, !test.wantNil)
		}
		if state != test.wantState {
			t.Errorf("TestResetFor(%s): got state %d after reset, want %d", test.name, state, test.wantState)
		}
	}
}

// applyReset calls reset on o when reset is not nil, then returns state() and whether reset was not nil.
func applyReset[T any](reset func(o *Owned[T]), o *Owned[T], state func() int) (int, bool) {
	if reset != nil {
		reset(o)
	}
	return state(), reset != nil
}

// sliceResetter is a buffer-shaped value type with a pointer receiver Reset(). Converting it to an interface copies
// its slice header to the heap, unlike a small integer, which Go boxes from a static table without allocating.
type sliceResetter struct {
	b []byte
}

func (s *sliceResetter) Reset() {
	s.b = s.b[:0]
}

// TestOwnedPoolValueTypeHitAllocs pins that a pool hit on a value type allocates nothing: Put must not convert the
// value to an interface to find Reset(), which would copy it to the heap on every Put.
func TestOwnedPoolValueTypeHitAllocs(t *testing.T) {
	// Sequential: it measures allocations.
	if raceEnabled {
		t.Skip("the race detector makes sync.Pool drop values at random, so a hit cannot be guaranteed")
	}
	ctx := t.Context()

	p := NewOwnedPool(ctx, "", func() sliceResetter { return sliceResetter{b: make([]byte, 0, 64)} })
	warm := NextOwner()
	p.Put(ctx, warm, p.Get(ctx, warm))

	got := testing.AllocsPerRun(1000, func() {
		o := NextOwner()
		v := p.Get(ctx, o)
		s := v.Ptr(o)
		s.b = append(s.b, 1)
		p.Put(ctx, o, v)
	})
	if got != 0 {
		t.Errorf("TestOwnedPoolValueTypeHitAllocs: got %v allocs per Get+Put of a value type, want 0", got)
	}
}
