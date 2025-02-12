package worker

import (
	"github.com/gostdlib/base/values/generics/promises"
)

// Response is a response to some type of call that contains the value and an error.
type Response[T any] = promises.Response[T]

// Promise is a promise that can be used to return a value from a goroutine. It is a simple wrapper around a channel
// that can be used to return a value from a goroutine. This is designed to be used with the
// base/concurrency/sync.Pool type. It will automatically call Reset() when put back in the pool and should save
// memory by not allocation a new Promise or channel.
type Promise[I, O any] = promises.Promise[I, O]

type opts struct {
	pool any
}

// PromiseOption is an option for NewPromise.
type PromiseOption = promises.Option

// WithPool sets a *sync.Pool[chan Response[T]] that is used to recycle the channel in the promise after it
// has had Get() called on it. If this is not the correct type, it will panic.
func WithPool(p any) PromiseOption {
	return promises.WithPool(p)
}

// NewPromise creates a new Promise.
func NewPromise[I, O any](in I, options ...PromiseOption) Promise[I, O] {
	return promises.NewPromise[I, O](in, options...)
}
