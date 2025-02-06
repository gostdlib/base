package context

import (
	"context"
	"log"
	"testing"

	"github.com/gostdlib/base/concurrency/background"
	"github.com/gostdlib/base/telemetry/otel/metrics"

	"go.opentelemetry.io/otel/metric"
)

func TestBackground(t *testing.T) {
	ctx := Background()

	if ctx.Value(loggerKey{}) != log.Default() {
		t.Errorf("TestBackground: Logger not attached to context")
	}

	if ctx.Value(metricsKey{}) != metrics.Default() {
		t.Errorf("TestBackground: Metrics not attached to context")
	}
}

func TestLog(t *testing.T) {
	defaultLogger := log.Default()

	tests := []struct {
		name string
		ctx  context.Context
		want *log.Logger
	}{
		{
			name: "LoggerAttached",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, loggerKey{}, defaultLogger)
			}(),
			want: defaultLogger,
		},
		{
			name: "NoLoggerAttached",
			ctx:  context.Background(),
			want: defaultLogger,
		},
		{
			name: "InvalidLoggerType",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, loggerKey{}, "invalid")
			}(),
			want: defaultLogger,
		},
	}

	for _, test := range tests {
		got := Log(test.ctx)
		if got != test.want {
			t.Errorf("TestLog() = %v, want %v", got, test.want)
		}
	}
}

func TestMetrics(t *testing.T) {
	defaultProvider := metrics.Default()

	tests := []struct {
		name string
		ctx  context.Context
		want metric.MeterProvider
	}{
		{
			name: "MetricsProviderAttached",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, metricsKey{}, defaultProvider)
			}(),
			want: defaultProvider,
		},
		{
			name: "NoAuditClientAttached",
			ctx:  context.Background(),
			want: defaultProvider,
		},
		{
			name: "InvalidAuditClientType",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, metricsKey{}, "invalid")
			}(),
			want: defaultProvider,
		},
	}

	for _, test := range tests {
		got := MeterProvider(test.ctx)
		if got != test.want {
			t.Errorf("TestMetrics(%s): MeterProvider() = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestTasks(t *testing.T) {
	defaultTasks := background.Default()

	tests := []struct {
		name string
		ctx  context.Context
		want *background.Tasks
	}{
		{
			name: "ClientAttached",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, tasksKey{}, defaultTasks)
			}(),
			want: defaultTasks,
		},
		{
			name: "NoAuditClientAttached",
			ctx:  context.Background(),
			want: defaultTasks,
		},
		{
			name: "InvalidAuditClientType",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, tasksKey{}, "invalid")
			}(),
			want: defaultTasks,
		},
	}

	for _, test := range tests {
		got := Tasks(test.ctx)
		if got != test.want {
			t.Errorf("TestTasks() = %v, want %v", got, test.want)
		}
	}
}

func TestShouldTrace(t *testing.T) {
	ctx := context.Background()

	if ShouldTrace(ctx) {
		t.Fatalf("TestShouldTrace: ShouldTrace() = true, want false")
	}

	ctx = SetShouldTrace(ctx, true)
	if !ShouldTrace(ctx) {
		t.Fatalf("TestShouldTrace: ShouldTrace() = false, want true")
	}
}
