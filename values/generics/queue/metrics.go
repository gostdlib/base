package queue

import (
	"sync/atomic"
	"time"

	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/telemetry/otel/trace/span"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

// queueMetrics holds the OTEL instruments shared by every operation on a Queue. It is
// built once in New and panics on instrument-construction error, matching
// github.com/gostdlib/base/concurrency/background.
type queueMetrics struct {
	// ops is the numbers of operations that have occurred, such as Push, Pop, ...
	ops metric.Int64Counter
	// errs are the numbers of errors encountered by ops.
	errs metric.Int64Counter
	// latency is the latency for various ops.
	latency metric.Float64Histogram
	// depth is the current queue depth.
	depth metric.Int64UpDownCounter
	// lastDepth was the last depth recorded.
	lastDepth atomic.Int64
}

func newQueueMetrics(m metric.Meter) *queueMetrics {
	ops, err := m.Int64Counter(
		"queue.operations",
		metric.WithDescription("Total number of queue operations invoked."),
	)
	if err != nil {
		panic(err)
	}
	errs, err := m.Int64Counter(
		"queue.operation.errors",
		metric.WithDescription("Total number of queue operations that returned an error."),
	)
	if err != nil {
		panic(err)
	}
	latency, err := m.Float64Histogram(
		"queue.operation.duration",
		metric.WithDescription("Duration of queue operations."),
		metric.WithUnit("s"),
	)
	if err != nil {
		panic(err)
	}
	depth, err := m.Int64UpDownCounter(
		"queue.depth",
		metric.WithDescription("Current number of items in the queue."),
	)
	if err != nil {
		panic(err)
	}
	return &queueMetrics{ops: ops, errs: errs, latency: latency, depth: depth}
}

// instrument starts (or no-ops) a span for op and records the operation count. The
// returned func finishes it: it records latency, and on a non-nil *errp increments the
// error counter and marks the span. Use as:
//
//	ctx, done := q.instrument(ctx, "Push")
//	defer func() { done(&err) }()
//
// errp must be non-nil (every caller passes the address of a named return).
//
// When the queue has no name (q.met == nil) telemetry is disabled and this is a no-op
// returning ctx unchanged. Otherwise span.New still no-ops unless ctx already carries a
// recording span and the meter no-ops unless an exporter is configured.
func (q *Queue[T]) instrument(ctx context.Context, op string) (context.Context, func(*error)) {
	if q.met == nil {
		return ctx, func(*error) {}
	}
	start := time.Now()
	ctx, sp := context.NewSpan(ctx, span.WithName("github.com/gostdlib/base/values/generics/queue.Queue."+op))
	attrs := metric.WithAttributes(
		attribute.String("queue.name", q.name),
		attribute.String("queue.operation", op),
	)
	q.met.ops.Add(ctx, 1, attrs)

	return ctx, func(errp *error) {
		q.met.latency.Record(ctx, time.Since(start).Seconds(), attrs)
		err := *errp
		if err != nil {
			q.met.errs.Add(ctx, 1, attrs)
			if sp.IsRecording() {
				sp.Span.RecordError(err)
				sp.Status(codes.Error, err.Error())
			}
		}
		// End unconditionally: always end what you started (OTEL convention).
		// span.Span.End is itself a no-op on a non-recording span.
		sp.End()
	}
}

// recordDepth emits the change in queue length as a delta on the depth UpDownCounter.
// The atomic Swap makes the running total self-correcting, so it converges to the true
// Len even under concurrent mutation. depth is a queue-level quantity, so it carries
// only the queue.name attribute (no per-operation split).
func (q *Queue[T]) recordDepth(ctx context.Context) {
	if q.met == nil {
		return
	}
	cur := q.backing.Len()
	old := q.met.lastDepth.Swap(cur)
	q.met.depth.Add(ctx, cur-old, metric.WithAttributes(attribute.String("queue.name", q.name)))
}
