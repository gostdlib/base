/*
Package chans provides common operations on channels that normally have to be wrapped in select statements. This
does not replace complex select/channel operations, but for the common case of getting, putting and iterating
values on channels while using context or default statements, this package provides a simple interface.
*/
package chans

import (
	"fmt"
	"iter"

	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/values/chans/dynamic"
)

// Get returns the next value from ch. ok == false if the channel is closed or the context is done. If ch is nil, it panics.
func Get[T any](ctx context.Context, ch <-chan T) (v T, ok bool) {
	if ch == nil {
		panic("cannot receive from a nil channel")
	}
	select {
	case <-ctx.Done():
		var v T
		return v, false
	case v, ok := <-ch:
		if !ok {
			var v T
			return v, false
		}
		return v, true
	}
}

// Put sends a value to ch. If the context is done, it returns ok == false. If ch is nil, it panics.
func Put[T any](ctx context.Context, ch chan<- T, v T) (ok bool) {
	if ch == nil {
		panic("cannot send to a nil channel")
	}
	select {
	case <-ctx.Done():
		return false
	case ch <- v:
		return true
	}
}

// TryPut sends a value to ch if it is not full. If ch is nil, it panics. It returns true if the value was sent,
// false if the channel was full.
func TryPut[T any](ch chan<- T, v T) (ok bool) {
	if ch == nil {
		panic("cannot send to a nil channel")
	}
	select {
	case ch <- v:
		return true
	default:
		return false
	}
}

// Fill sends values from seq to ch until the context is done or the sequence is exhausted. If the context is done,
// Fill returns the value it pulled from seq but could not deliver and done == false. That value is no
// longer in seq, so a caller that cares about it must handle it. On success it returns the zero value of T and
// done == true.
func Fill[T any](ctx context.Context, ch chan<- T, seq iter.Seq[T]) (undelivered T, done bool) {
	if ch == nil {
		panic("cannot send to a nil channel")
	}
	for v := range seq {
		if ctx.Err() != nil {
			return v, false
		}
		select {
		case <-ctx.Done():
			return v, false
		case ch <- v:
		}
	}
	return undelivered, true
}

// TryGet returns the next value from ch if available. ok is true if a value was received. closed is true if ch is
// closed and drained, which lets a caller polling TryGet detect termination instead of spinning forever. On closed
// or empty channels, v is the zero value of T. If ch is nil, it panics.
func TryGet[T any](ch <-chan T) (v T, ok, closed bool) {
	if ch == nil {
		panic("cannot receive from a nil channel")
	}
	select {
	case v, ok = <-ch:
		return v, ok, !ok
	default:
		return v, false, false
	}
}

// Iter returns a sequence that yields values from ch until the context is done or ch is closed. If ch is nil,
// it panics.
// Check the context for cancellation if you want to know if the iteration was stopped due to context cancellation.
func Iter[T any](ctx context.Context, ch <-chan T) iter.Seq[T] {
	if ch == nil {
		panic("cannot receive from a nil channel")
	}
	return func(yield func(T) bool) {
		for {
			select {
			case <-ctx.Done():
				return
			case v, ok := <-ch:
				if !ok {
					return
				}
				if !yield(v) {
					return
				}
			}
		}
	}
}

type collectOptions struct {
	limit int
}

// CollectOption is an option for Collect.
type CollectOption func(collectOptions) collectOptions

// WithLimit sets a limit on the number of values to collect from the channel. If the limit is less than 1,
// it panics.
func WithLimit(limit int) CollectOption {
	return func(o collectOptions) collectOptions {
		if limit < 1 {
			panic("WithLimit: limit must be >= 1")
		}
		o.limit = limit
		return o
	}
}

// Collect collects all values from ch until it is closed or the context is done. It returns a slice of the collected
// values and done == true if the channel was closed. If done == false either the collection limit was reached
// via WithLimit() or the context was done, in which case it returns the values collected so far.
// Be careful, collecting without a limit or a time limited context can lead to unbounded memory
// usage and potentially deadlock if ch is never closed. If ch is nil, it panics.
func Collect[T any](ctx context.Context, ch <-chan T, options ...CollectOption) (v []T, done bool) {
	if ch == nil {
		panic("cannot receive from a nil channel")
	}
	opts := collectOptions{}
	for _, opt := range options {
		opts = opt(opts)
	}

	var result []T
	for {
		select {
		case <-ctx.Done():
			return result, false
		case v, ok := <-ch:
			if !ok {
				return result, true
			}
			result = append(result, v)
		}
		if opts.limit > 0 && len(result) >= opts.limit {
			return result, false
		}
	}
}

// Merge merges the output of multiple channel outputs into a single channel. The returned channel is closed when
// all input channels are closed.
func Merge[T any](chs ...<-chan T) <-chan T {
	ctx := context.Background()
	if len(chs) == 0 {
		panic("Merge: no channels provided")
	}
	if len(chs) > 65533 {
		panic("Merge: too many channels provided, maximum is 65533")
	}
	for _, ch := range chs {
		if ch == nil {
			panic("Merge: input channel is nil")
		}
	}

	// A channel passed more than once is registered once, so its values are yielded once. AddRecvAll rejects a
	// repeat within a batch, so the collapse happens here explicitly rather than by discarding an error.
	unique := make([]<-chan T, 0, len(chs))
	seen := make(map[<-chan T]bool, len(chs))
	for _, ch := range chs {
		if seen[ch] {
			continue
		}
		seen[ch] = true
		unique = append(unique, ch)
	}

	out := make(chan T, 1)
	s, err := dynamic.New[T](dynamic.WithPreallocate(len(unique)))
	if err != nil {
		panic(err)
	}
	// One snapshot copy for the whole batch. Registering with AddRecv in a loop is O(len(unique)^2).
	if err := s.AddRecvAll(func(_ context.Context, v T) { out <- v }, unique...); err != nil {
		panic(fmt.Sprintf("Merge: %s", err))
	}
	_ = context.Pool(ctx).Default().Submit(
		ctx,
		func() {
			defer close(out)
			for range s.All(ctx) {
				if s.Len() == 0 {
					return
				}
			}
		},
	)
	return out
}

// Tee copies values from ch to all output channels. It closes the output channels when ch is closed. Context
// cancellation has no effect on the operation of Tee. A blocked reader will stall the operation of Tee,
// so this is not a good choice for broadcasting to multiple readers, instead use gostdlib/concurrency/broadcast.
// If any channel is nil, Tee panics. If the same output channel is provided multiple times,
// Tee will panic when it tries to close the channel multiple times.
func Tee[T any](ch <-chan T, out ...chan<- T) {
	ctx := context.Background()
	if ch == nil {
		panic("Tee: input channel is nil")
	}
	if len(out) == 0 {
		panic("Tee: no output channels provided")
	}
	for _, o := range out {
		if o == nil {
			panic("Tee: output channel is nil")
		}
	}

	_ = context.Pool(ctx).Default().Submit(
		ctx,
		func() {
			for v := range ch {
				for _, o := range out {
					o <- v
				}
			}
			for _, o := range out {
				close(o)
			}
		},
	)
}
