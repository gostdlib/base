package sync

import (
	"context"
	"reflect"
	"sync/atomic"
)

// owner is the last owner ID handed out by NextOwner(). 0 is never handed out and means "unowned".
var owner atomic.Uint64

// NextOwner returns a new, unique owner ID. It never returns 0. This is safe for concurrent use.
func NextOwner() uint64 {
	return owner.Add(1)
}

// mustOwner panics if owner is 0, which means "unowned" and is never a valid caller.
func mustOwner(owner uint64) {
	if owner == 0 {
		panic("sync: owner cannot be 0")
	}
}

// Owned holds a value that belongs to a single owner. It is used to detect when a value from an OwnedPool is
// accessed or returned by something other than the owner it was handed to, such as after it was put back in the pool.
type Owned[T any] struct {
	v     T
	owner atomic.Uint64
}

// V returns the value after validating that it is owned by owner. If it is not, it panics.
func (o *Owned[T]) V(owner uint64) T {
	o.Validate(owner)
	return o.v
}

// Ptr returns a pointer to the value after validating that it is owned by owner. If it is not, it panics.
// Use it to change a value type in place: V returns a copy, so a pool of value types (rather than pointers) needs Ptr
// to write, and storing the value inside the Owned saves the separate allocation a pointer type costs on every pool
// miss. The pointer is only valid while owner holds the value. Keeping it past Put or Transfer is not detected, so
// call Ptr at each use instead of storing its result.
func (o *Owned[T]) Ptr(owner uint64) *T {
	o.Validate(owner)
	return &o.v
}

// Validate checks if the value is owned by owner. If not, or owner is 0, it panics.
func (o *Owned[T]) Validate(owner uint64) {
	mustOwner(owner)
	if o.owner.Load() != owner {
		panic("sync.Owned: value is not owned by the caller")
	}
}

// Transfer transfers ownership of the value from oldOwner to newOwner. If the value is not owned by oldOwner,
// or either owner is 0, it panics.
func (o *Owned[T]) Transfer(oldOwner, newOwner uint64) {
	mustOwner(oldOwner)
	mustOwner(newOwner)
	if !o.owner.CompareAndSwap(oldOwner, newOwner) {
		panic("sync.Owned: value is not owned by the caller")
	}
}

// resetFor returns the function that OwnedPool.Put uses to reset a T in place, or nil when T has no Reset() method. It
// is decided once, when the pool is made, so Put never converts the value to an interface to look for Reset(): for a
// value type that conversion copies the value to the heap, which is an allocation per Put. Resetting through &o.v,
// which already lives on the heap inside the Owned, is what lets a value type with a pointer receiver Reset() be reset
// without the allocation Pool.Put pays for it. It probes zero values only, so it never calls the pool's New function.
// The returned function is built once per pool: a function literal in a generic function captures the type's
// dictionary, so building it allocates once there, and calling it on Put allocates nothing.
func resetFor[T any]() func(o *Owned[T]) {
	// An interface T can hold values that do and do not implement Resetter, so it is checked per value. Converting an
	// interface value to any does not allocate.
	if reflect.TypeFor[T]().Kind() == reflect.Interface {
		return func(o *Owned[T]) {
			if r, ok := any(o.v).(Resetter); ok {
				r.Reset()
			}
		}
	}
	var zero T
	// *T implements Resetter for a pointer receiver and for a value receiver, since the method set of *T includes the
	// methods of T. Converting &o.v to an interface does not allocate.
	if _, ok := any(&zero).(Resetter); ok {
		return func(o *Owned[T]) {
			any(&o.v).(Resetter).Reset()
		}
	}
	// Only T implements Resetter, which happens when T is a pointer-shaped type such as *X. Converting a pointer-shaped
	// value to an interface does not allocate.
	if _, ok := any(zero).(Resetter); ok {
		return func(o *Owned[T]) {
			any(o.v).(Resetter).Reset()
		}
	}
	return nil
}

// OwnedPool is a Pool whose values have an ownership model. Each value handed out by Get() is tagged with an owner
// ID and can only be accessed or returned to the pool by that owner, which catches use-after-Put and double Put bugs.
// If the stored type implements Resetter, Reset() is called when the value is returned to the pool.
type OwnedPool[T any] struct {
	pool *Pool[*Owned[T]]
}

// NewOwnedPool creates a new OwnedPool for use. A "name" is used to create a new meter with the name:
//
//	"[package path]/[package name]:sync.Pool([type stored])/[name]".
//
// If you are providing a type that implementing Resetter, use the ./reset package to validate your reset in tests.
func NewOwnedPool[T any](ctx context.Context, name string, n func() T, options ...Option) *OwnedPool[T] {
	// NewPool() names the meter after its caller, which is us. Move it up one level to our caller.
	options = append(
		options,
		func(o opts) (opts, error) {
			o.prefixLevel++
			return o, nil
		},
	)

	pool := NewPool(
		ctx,
		name,
		func() *Owned[T] {
			return &Owned[T]{v: n()}
		},
		options...,
	)
	if r := resetFor[T](); r != nil {
		pool.reset = func(o *Owned[T]) *Owned[T] {
			r(o)
			return o
		}
	}
	return &OwnedPool[T]{pool: pool}
}

// Get returns a value from the pool owned by owner. owner should come from NextOwner(). If owner is 0 or
// the value is already owned, it panics.
func (p *OwnedPool[T]) Get(ctx context.Context, owner uint64) *Owned[T] {
	mustOwner(owner)
	v := p.pool.Get(ctx)
	if !v.owner.CompareAndSwap(0, owner) {
		panic("sync.OwnedPool: value from pool is already owned")
	}
	return v
}

// Put returns a value to the pool. If v is not owned by owner, or owner is 0, it panics. This includes putting a
// value that was already returned to the pool.
func (p *OwnedPool[T]) Put(ctx context.Context, owner uint64, v *Owned[T]) {
	mustOwner(owner)
	if !v.owner.CompareAndSwap(owner, 0) {
		panic("sync.OwnedPool: value is not owned by the caller")
	}
	// The inner pool resets v in place (see NewOwnedPool) before it stores v.
	p.pool.Put(ctx, v)
}
