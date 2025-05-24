/*
Package slices provides concurrency types for operating on slices.

Transform provides concurrent access or mutation of a slice in parallel. This is a lock free implementation.

This should only be used if the operation involving the slice is time consuming. If the operation is quick and
only requires doing work with memory, especially if it can use the cache, the overhead of parrallelism will be
greater than the performance gain. For those cases simply use a "for" loop.

Here is an example using the Transformer to mutate a slice of integers. This will double each element in the slice:

	s := []int{1, 2, 3, 4, 5}
	slices.Transform[int](
		ctx,
		context.Pool(ctx).Limited(-1),
		func(ctx context.Context, index int, currentValue int) (int, error) {
			return currentValue * 2, nil
		},
	)

This example uses the worker pool attached to the context and will use up to NumCPU workers. But a note here
about speed again:

BenchmarkTransform/100(Transform)-10               10000            101300 ns/op
BenchmarkTransform/100(Loop)-10                 31800469                37.61 ns/op
BenchmarkTransform/1000(Transform)-10               1122           1106045 ns/op
BenchmarkTransform/1000(Loop)-10                 3725611               323.2 ns/op
BenchmarkTransform/10000(Transform)-10               100          11047195 ns/op
BenchmarkTransform/10000(Loop)-10                 381148              3144 ns/op
BenchmarkTransform/100000(Transform)-10               12         108119351 ns/op
BenchmarkTransform/100000(Loop)-10                 33697             35688 ns/op
BenchmarkTransform/1000000(Transform)-10               1        1008466833 ns/op
BenchmarkTransform/1000000(Loop)-10                 2916            409382 ns/op
BenchmarkTransform/10000000(Transform)-10              1        10722167250 ns/op
BenchmarkTransform/10000000(Loop)-10                 290           4142692 ns/op

A standard loop will decimate a concurrent transform. For speed, you need to be doing complicated work that
does more than access main memory. This will almost always involve IO.

We can also use Transform to create a new slice. Here is an example that creates a new slice of integers:

	s := []int{1, 2, 3, 4, 5}
	newS := make([]int, len(s))

	slices.Transform[int](
		ctx,
		context.Pool(ctx).Limited(-1),
		func(ctx context.Context, index int, currentValue int) (int, error) {
			newS[index] = currentValue * 2
			return currentValue, nil
		},
	)

This provided us with a new slice of integers that is double the original while retaining the original slice.
*/
package slices

import (
	"context"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/concurrency/worker"
	"github.com/gostdlib/base/telemetry/otel/trace/span"
)

// Transformer is a function that is called on each element in a slice.
// This passes the index of the element and the value of the element. The function
// returns a new value or an error. If an error is returned, the value is not updated.
type Transformer[T any] func(ctx context.Context, index int, currentValue T) (newValue T, err error)

type runOpts struct {
	stopOnErr bool
}

// TransformOption is a function that modifies the runOpts.
type TransformOption func(runOpts) (runOpts, error)

// WithStopOnErr causes any error returned by a Dicer to stop execution.
func WithStopOnErr() TransformOption {
	return func(o runOpts) (runOpts, error) {
		o.stopOnErr = true
		return o, nil
	}
}

// Transform calls the Tranformer for each element in "s". This can be used to mutate every element
// in a slice, send values to a channel, create a new slice, etc.
// Errors returned here will be the sync.Errors type with IndexErr provided. This will not
// stop the run from completing unless WithStopOnErr() option was provided.
// This is a lock free implementation.
func Transform[T any](ctx context.Context, p *worker.Limited, d Transformer[T], s []T, options ...TransformOption) error {
	if len(s) == 0 {
		return nil
	}

	spanner := span.Get(ctx)

	opts := runOpts{}
	var err error
	for _, o := range options {
		opts, err = o(opts)
		if err != nil {
			return err
		}
	}

	g := p.Group()

	var cancel = func() {}
	if opts.stopOnErr {
		ctx, cancel = context.WithCancel(ctx)
		g.CancelOnErr = cancel
	}

	for i := 0; i < len(s); i++ {
		i := i

		if ctx.Err() != nil {
			return ctx.Err()
		}

		g.Go(
			ctx,
			func(ctx context.Context) error {
				startV := s[i]
				newV, err := d(ctx, i, startV)
				if err != nil {
					return err
				}
				s[i] = newV

				return nil
			},
			sync.WithIndex(i),
		)
	}
	if err := g.Wait(ctx); err != nil {
		spanner.Span.RecordError(err)
		return err
	}
	return nil
}
