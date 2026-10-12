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
