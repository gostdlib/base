// Package result provides a simple way to represent the eventual outcome of some operation. This is useful
// when multiple goroutines could be waiting for the same result.
//
// Use is simple:
//
//	r := result.New[int]()
//	go func() {
//		// do some work
//		v, err := r.Wait(context.Background())
//		// do more work
//	}()
//
//	go func() {
//		// do some work
//		v, err := r.Wait(context.Background())
//		// do more work
//	}()
//
//	// Do some work
//
//	r.Set(42, nil)
package result

import "github.com/gostdlib/base/context"

// Value is the eventual outcome of some operation.
// Set must be called at most once. Wait is safe to call concurrently.
type Value[T any] struct {
	done chan struct{}
	v    T
	err  error
}

// New creates a Value.
func New[T any]() *Value[T] {
	return &Value[T]{done: make(chan struct{})}
}

// Set stores the response and error. This will unblock Done() and Wait() calls.
// Calling Set more than once panics.
func (r *Value[T]) Set(v T, err error) {
	r.v = v
	r.err = err
	close(r.done)
}

// Done returns a channel that is closed once Set has been called.
func (r *Value[T]) Done() <-chan struct{} {
	return r.done
}

// Wait blocks until Set is called or ctx is canceled and returns the results
// of Set(). This can be called concurrently.
func (r *Value[T]) Wait(ctx context.Context) (T, error) {
	select {
	case <-ctx.Done():
		var z T
		return z, context.Cause(ctx)
	case <-r.done:
		return r.v, r.err
	}
}
