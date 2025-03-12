/*
Package slices provides concurrency types for operating on slices.

Slicer provides concurrent access or mutation of a slice in parallel. This is a lock free implementation.

This should only be used if the operation involving the slice is time consuming. If the operation is quick,
it is almost certain that the overhead of parrallelism will be greater than the performance gain. For those cases
simply use a "for" loop.

Here is an example using the Slicer to mutate a slice of integers. This will double each element in the slice:

	s := []int{1, 2, 3, 4, 5}
	slices.Slicer[int]{
		Dicer: func(ctx context.Context, index int, currentValue int) (int, error) {
			return currentValue * 2, nil
		},
	}.Run(ctx, s)

This example uses the default worker pool and will use up to runtime.NumCPU() workers. You can alter that
by providing your own Group object to the Slicer.Group field.

We can also use the Slicer to create a new slice. Here is an example that creates a new slice of integers:

	s := []int{1, 2, 3, 4, 5}
	newS := make([]int, len(s))

	slices.Slicer[int]{
		Dicer: func(ctx context.Context, index int, currentValue int) (int, error) {
			newS[index] = currentValue * 2
			return currentValue, nil
		},
	}.Run(ctx, s)

This provided us with a new slice of integers that is double the original while retaining the original slice.
*/
package slices

import (
	"context"
	"runtime"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/concurrency/worker"
	"github.com/gostdlib/base/telemetry/otel/trace/span"
)

// Dicer is a function that is called on each element in a slice.
// This passes the index of the element and the value of the element. The function
// returns a new value or an error. If an error is returned, the value is not updated.
type Dicer[T any] func(ctx context.Context, index int, currentValue T) (newValue T, err error)

// Slicer is used to run a function on each element of a slice in parallel.
type Slicer[T any] struct {
	// Group provides a sync.Group that can be used to control the concurrency of the run.
	// If this is nil, one will be created off the default worker pool using runtime.NumCPU() workers.
	Group *sync.Group
	// Dicer called on every slice element at a given index with a given value.
	Dicer Dicer[T]
}

type runOpts struct {
	stopOnErr bool
}

// RunOption is a function that modifies the runOpts.
type RunOption func(runOpts) (runOpts, error)

// WithStopOnErr causes any error returned by a Dicer to stop execution.
func WithStopOnErr() RunOption {
	return func(o runOpts) (runOpts, error) {
		o.stopOnErr = true
		return o, nil
	}
}

// Run calls the Accessor for each element in "s". This can be used to mutate every element
// in a slice, send values to a channel, create a new slice, etc.
// If .Group isn't provided, up to runtime.NumCPU() will be used.
// Errors returned here will be the sync.Errors type with IndexErr provided. This will not
// stop the run from completing unless WithStopOnErr() option was provided.
// This is a lock free implementation.
func (d *Slicer[T]) Run(ctx context.Context, s []T, options ...RunOption) error {
	spanner := span.Get(ctx)

	opts := runOpts{}
	var err error
	for _, o := range options {
		opts, err = o(opts)
		if err != nil {
			return err
		}
	}

	if len(s) == 0 {
		return nil
	}

	if d.Group == nil {
		p := worker.Default().Limited(runtime.NumCPU())
		g := p.Group()
		d.Group = &g
	}

	var cancel = func() {}
	if opts.stopOnErr {
		ctx, cancel = context.WithCancel(ctx)
		d.Group.CancelOnErr = cancel
	}

	for i := 0; i < len(s); i++ {
		i := i

		if ctx.Err() != nil {
			return ctx.Err()
		}

		d.Group.Go(
			ctx,
			func(ctx context.Context) error {
				startV := s[i]
				newV, err := d.Dicer(ctx, i, startV)
				if err != nil {
					return err
				}
				s[i] = newV

				return nil
			},
			sync.WithIndex(i),
		)
	}
	if err := d.Group.Wait(ctx); err != nil {
		spanner.Span.RecordError(err)
		return err
	}
	return nil
}
