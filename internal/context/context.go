// Package context exists to avoid some import cycles.
package context

import (
	"context"

	"github.com/gostdlib/base/telemetry/otel/metrics"

	"go.opentelemetry.io/otel/metric"
	metricsnoop "go.opentelemetry.io/otel/metric/noop"
)

// MetricsKey is a key for the context that stores a metrics.MeterProvider.
type MetricsKey struct{}

// ShouldTraceKey is a key for the context that stores a bool.
type ShouldTraceKey struct{}

// MeterProvider returns a metric.MeterProvider attached to the context. If no meter provider is attached,
// it returns metrics.Default(). This may be a noop provider.
func MeterProvider(ctx context.Context) metric.MeterProvider {
	a := ctx.Value(MetricsKey{})
	if a == nil {
		return metrics.Default()
	}
	l, ok := a.(metric.MeterProvider)
	if !ok {
		return metricsnoop.NewMeterProvider()
	}
	return l
}

// ShouldTrace returns true if the request has had SetShouldTrace called on it.
func ShouldTrace(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if ctx.Value(ShouldTraceKey{}) == nil {
		return false
	}
	v, ok := ctx.Value(ShouldTraceKey{}).(bool)
	if !ok {
		return false
	}
	return v
}

// SetShouldTrace attaches a boolean value to the context to indicate if the request should be traced.
// This is not usually used by a service, but by the middleware to determine if the request should
// be traced. This only works if done before the trace is started.
func SetShouldTrace(ctx context.Context, b bool) context.Context {
	return context.WithValue(ctx, ShouldTraceKey{}, b)
}
