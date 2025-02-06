package promises

import (
	"context"

	"github.com/gostdlib/base/concurrency/sync"
)

// Response is a response to some type of call that contains the value and an error.
// NOTE: Remove this once Go 1.24 is released. This is here until we can do a type
// assertion on a generic type. This should be equal to generics/common.Response.
type Response[T any] struct {
	// V is the value.
	V T
	// Err is the error.
	Err error
}

// Promise is a promise that can be used to return a value from a goroutine. It is a simple wrapper around a channel
// that can be used to return a value from a goroutine. This is designed to be used with the
// base/concurrency/sync.Pool type. It will automatically call Reset() when put back in the pool and should save
// memory by not allocation a new Promise or channel.
// NOTE: Remove this once Go 1.24 is released. This is here until we can do a type
// assertion on a generic type. This should be equal to generics/common.Response.
type Promise[I, O any] struct {
	// In is the value being sent.
	In I

	// v is the value returned by the promise.
	v chan Response[O]

	pool *sync.Pool[chan Response[O]]
}

type opts struct {
	pool any
}

// PromiseOption is an option for NewPromise.
type Option func(o opts) opts

// WithPool sets a *sync.Pool[chan Response[T]] that is used to recycle the channel in the promise after it
// has had Get() called on it. If this is not the correct type, it will panic.
// Note that using a pool introduces a tiny bit of delay, but saves an allocation and memory. Use only if
// you are creating a lot of promises.
func WithPool(p any) Option {
	return func(o opts) opts {
		o.pool = p
		return o
	}
}

// NewPromise creates a new Promise.
func NewPromise[I, O any](in I, options ...Option) Promise[I, O] {
	opts := opts{}

	for _, o := range options {
		opts = o(opts)
	}

	promise := Promise[I, O]{In: in}
	if opts.pool != nil {
		// Here is our runtime check.
		p, ok := opts.pool.(*sync.Pool[chan Response[O]])
		if !ok {
			panic("pool must be a *sync.Pool[chan T]")
		}
		// Assign to our type specific variable.
		promise.pool = p
	}

	if promise.pool == nil {
		promise.v = make(chan Response[O], 1)
	} else {
		promise.v = promise.pool.Get(context.Background())
	}
	return promise
}

// Get returns the value from the promise. If the promise has not been resolved, it will block until it is or
// the context is done. Get should only be called once per promise unless you received an error and the error
// was a context error.
func (p *Promise[I, O]) Get(ctx context.Context) (Response[O], error) {
	if p.v == nil {
		panic("promise was not created with NewPromise")
	}
	select {
	case v := <-p.v:
		if p.pool != nil {
			p.pool.Put(context.Background(), p.v)
		}
		return v, nil
	case <-ctx.Done():
		return Response[O]{}, context.Cause(ctx)
	}
}

// Set sets the value of the promise. If the promise has already had Set() called and the value is not read,
// this will return an error. However, you should never call Set() more than once on a promise.
func (p *Promise[I, O]) Set(ctx context.Context, v O, err error) error {
	if p.v == nil {
		panic("promise was not created with NewPromise")
	}
	select {
	case p.v <- Response[O]{V: v, Err: err}:
	default:
		panic("bug in your program: promise was already resolved with a call to Set()")
	}
	return nil
}

// Reset resets the promise. This implements the base/concurrency/sync.Resetter interface.
func (p *Promise[I, O]) Reset() {
	// This means this was broken to start, but we can at least fix it.
	if p.v == nil {
		p.v = make(chan Response[O], 1)
		return
	}

	var zeroValue I
	p.In = zeroValue

	// Make sure the channel is empty.
	select {
	case <-p.v:
	default:
	}
}
