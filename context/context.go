// Package context is a drop in replacement for the standard library's context package.
// It provides additional functionality to the context package, such as attaching various clients
// to the context when calling Background().  This should be called after init.Service() to ensure
// that the clients are initialized. All clients that are attached to the context should return a
// a value that is safe to call methods on.
package context

import (
	"context"
	"log/slog"

	"github.com/gostdlib/base/concurrency/background"
	"github.com/gostdlib/base/concurrency/worker"
	internalCtx "github.com/gostdlib/base/internal/context"
	ierr "github.com/gostdlib/base/internal/errors"
	"github.com/gostdlib/base/telemetry/log"
	"github.com/gostdlib/base/telemetry/otel/metrics"

	"go.opentelemetry.io/otel/metric"
	metricsnoop "go.opentelemetry.io/otel/metric/noop"
)

// Types for keys used to attach clients to the context.
type (
	loggerKey  struct{}
	poolKey    struct{}
	tasksKey   struct{}
	metricsKey = internalCtx.MetricsKey
)

// Background returns a non-nil, empty [Context]. It is never canceled, and has no deadline.
// It is typically used by the main function, initialization, and tests, and as the top-level
// Context for incoming requests. This differs from the Background() function in the context package
// in that it attaches various clients to the context. This currently attaches:
//
// - log.Default(), a *slog.Logger.
// - metrics.Default(), a metric.MeterProvider.
// - worker.Default(), a *worker.Pool.
// - background.Default(), a *background.Tasks.
//
// These can be accessed using the Audit()/Log()/Metrics functions.
func Background() Context {
	ctx := context.Background()
	return Attach(ctx)
}

// Attach attaches the audit, logger, and metrics clients to the context.
// This is generally not called directly, but is used by Background() and
// things like RPC packages that need to attach these to an already existing context.
func Attach(ctx Context) Context {
	ctx = WithValue(ctx, loggerKey{}, log.Default())
	ctx = WithValue(ctx, metricsKey{}, metrics.Default())
	ctx = WithValue(ctx, poolKey{}, worker.Default())
	ctx = WithValue(ctx, tasksKey{}, background.Default())
	return ctx
}

// Log returns the logger attached to the context. If no logger is attached, it returns log.Default().
func Log(ctx Context) *slog.Logger {
	a := ctx.Value(loggerKey{})
	if a == nil {
		return log.Default()
	}
	l, ok := a.(*slog.Logger)
	if !ok {
		return log.Default()
	}
	return l
}

// Meter returns a metric.Meter scoped to the package that calls context.Meter(). If you need to have a
// sub-namespace for a specific package, you should use the MeterProvider() function to get the meter provider.
// If no meter is attached to the context it returns a meter from metrics.Default(). This may be a noop Meter.
func Meter(ctx Context, opts ...metric.MeterOption) metric.Meter {
	const stackFrame = 3

	return MeterWithStackFrame(ctx, stackFrame, opts...)
}

// MeterWithStackFrame returns a metric.Meter scoped to the stack frame number provided by "sf".
// This is for uses by packages that use this underneath so they can get the write stack frame.
// Generally, you should be using Meter().
func MeterWithStackFrame(ctx Context, sf uint, opts ...metric.MeterOption) metric.Meter {
	a := ctx.Value(metricsKey{})
	if a == nil {
		return metrics.Default().Meter(metrics.MeterName(int(sf)), opts...)
	}
	l, ok := a.(metric.MeterProvider)
	if !ok {
		return metricsnoop.NewMeterProvider().Meter("")
	}
	return l.Meter(metrics.MeterName(int(sf)), opts...)
}

// MeterProvider returns a metric.MeterProvider attached to the context. If no meter provider is attached,
// it returns metrics.Default(). This may be a noop provider.
func MeterProvider(ctx Context) metric.MeterProvider {
	return internalCtx.MeterProvider(ctx)
}

// Pool returns the worker pool attached to the context. If no pool is attached, it returns worker.Default().
func Pool(ctx Context) *worker.Pool {
	a := ctx.Value(poolKey{})
	if a == nil {
		return worker.Default()
	}
	p, ok := a.(*worker.Pool)
	if !ok {
		return worker.Default()
	}
	return p
}

// Tasks returns a background.Tasks attached to the context. If not tasks are attached,
// it returns background.Default().
func Tasks(ctx Context) *background.Tasks {
	a := ctx.Value(tasksKey{})
	if a == nil {
		return background.Default()
	}
	t, ok := a.(*background.Tasks)
	if !ok {
		return background.Default()
	}
	return t
}

// SetShouldTrace attaches a boolean value to the context to indicate if the request should be traced.
// This is not usually used by a service, but by the middleware to determine if the request should
// be traced. This only works if done before the trace is started.
func SetShouldTrace(ctx context.Context, b bool) context.Context {
	return internalCtx.SetShouldTrace(ctx, b)
}

// ShouldTrace returns true if the request has had SetShouldTrace called on it.
func ShouldTrace(ctx context.Context) bool {
	return internalCtx.ShouldTrace(ctx)
}

// EOptions returns the error options attached to the context. If no options are attached, it returns nil.
// This allows for setting per call error options. These will override local options if the same options are set.
// An example of this is writing a traceback to errors on a specific call or all calls.
func EOptions(ctx context.Context) []ierr.EOption {
	return internalCtx.EOptions(ctx)
}

// SetEOptions attaches error options to the context. This allows for setting per call error options.
// These will override local options if the same options are set. An example of this is writing a traceback
// to errors on a specific call or all calls.
func SetEOptions(ctx context.Context, options ...ierr.EOption) context.Context {
	return internalCtx.SetEOptions(ctx, options...)
}
