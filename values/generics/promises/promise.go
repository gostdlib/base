/*
Package promises provides generic types for promises and response objects that are useful for
various asynchronous operations.

A Response object is a generic type that contains a value or an error. This is useful for Promises or
streams where we want to detect an error in the stream.

A Promise object provides a type where we send a value to be processed and access the result at a later time
after the result has been calculated. An RPC service that sends a request into a centralized pipeline and responds
when that is done is a good example of this.

This package provides two methods for creating Promise objects, New() or Maker{}.New(). For single Promises,
simply use New(). When creating lots of Promise objects, use Maker{}.New() which does reuse of the
underlying channel objects. Real time for using either is roughly equivalent, but underlying CPU time
drops significantly.

Here are some benchmarks:

	2025/05/24 11:20:03 TestMaker: Time taken for 1.09963175s
	2025/05/24 11:20:03 TestNewPromise: Time taken for 1.107532792s
	goos: darwin
	goarch: arm64
	pkg: github.com/gostdlib/base/values/generics/promises
	cpu: Apple M1 Max
	BenchmarkNew-10                 1000000000               0.6705 ns/op          0 B/op          0 allocs/op
	BenchmarkMaker-10               1000000000               0.0000042 ns/op               0 B/op          0 allocs/op

Using a promise is simple:

	maker := Maker[int, string]{PoolOptions: []sync.Option{sync.WithBuffer(10)}

	promises := make([]Promise[int, string], 10) // Track our promises

	// Create a promise and sent it to some channel to get processed.
	for i := 0; i < 10; i++ {
		promises[i] := maker.New(ctx, i)
		ch <-p
	}

	// ... Do some other stuff or not

	// Wait for each process to complete and get its value.
	for _, p := range promises {
		resp, err := p.Get(ctx)
		if err != nil {
			// Do something, this can only happen if ctx cancels or times out.
		}
		fmt.Println(resp)
	}

If you are just doing a one off promise, you can just use New():

	p := New[int, string](ctx, 1)
	go Process(ctx, p)

	// Do other stuff if you want.
	resp, err := p.Get(ctx)
	if err != nil {
		// Do something, this can only happen if ctx cancels or times out.
	}
	fmt.Println(p.Get(ctx))

In the case the operation can have an error, it is a good idea to use a Response:

	p := New[int, Response[string]](ctx, 1)
	go Process(ctx, p)

	// Do other stuff if you want.

	resp, err := p.Get(ctx)
	if err != nil {
		// Do something, this can only happen if ctx cancels or times out.
	}

	if resp.Err != nil {
		// Process() had an error, so you need to deal with that.
	}
	fmt.Println(resp.V)
*/
package promises

import (
	"context"

	"github.com/gostdlib/base/concurrency/sync"
)

// Response is a response to some type of call that contains the value and an error.
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
type Promise[I, O any] struct {
	// In is the value being sent.
	In I

	// v is the value returned by the promise.
	v chan Response[O]

	pool *sync.Pool[chan Response[O]]
}

// Maker is used to make promises. Use this when making a lot of promises to maximize reuse and
// reduce memory consumption.
type Maker[I, O any] struct {
	// PoolOptions provides options for the sync.Pool that is created on the first call to New().
	PoolOptions []sync.Option

	pool *sync.Pool[chan Response[O]]
	once sync.Once
}

type opts struct{}

// Option is an option for NewPromise.
type Option func(o opts) opts

// New creates a new Promise.
func (m *Maker[I, O]) New(ctx context.Context, in I, options ...Option) Promise[I, O] {
	m.once.Do(
		func() {
			m.pool = sync.NewPool(ctx, "", func() chan Response[O] { return make(chan Response[O], 1) }, m.PoolOptions...)
		},
	)
	return newPromise(in, m.pool.Get(ctx), options...)
}

// New creates a new Promise.
func New[I, O any](ctx context.Context, in I, options ...Option) Promise[I, O] {
	resp := make(chan Response[O], 1)

	promise := Promise[I, O]{In: in, v: resp}
	return promise
}

func newPromise[I, O any](in I, resp chan Response[O], options ...Option) Promise[I, O] {
	opts := opts{}

	for _, o := range options {
		opts = o(opts)
	}

	return Promise[I, O]{In: in, v: resp}
}

// Get returns the value from the promise. If the promise has not been resolved, it will block until it is or
// the context is done. Get should only be called once per promise. It will only error if the context is
// cancelled or timed out. In that case only can Get() be called again.
func (p *Promise[I, O]) Get(ctx context.Context) (Response[O], error) {
	if p.v == nil {
		panic("promise was not created with NewPromise or Maker.New()")
	}
	select {
	case v := <-p.v:
		if p.pool != nil {
			p.pool.Put(ctx, p.v)
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
		panic("promise was not created with New() or Maker{}.New()")
	}
	select {
	case p.v <- Response[O]{V: v, Err: err}:
	default:
		panic("bug in your program: promise was already resolved with a call to Set()")
	}
	return nil
}
