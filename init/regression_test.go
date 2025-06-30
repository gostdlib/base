package init

import (
	"fmt"
	"testing"

	"github.com/gostdlib/base/context"
	"github.com/prometheus/client_golang/prometheus"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

const subsystem = "test"

var (
	// runningCount is a gauge for tracking the number of running procedures.
	runningCount metric.Int64UpDownCounter
)

var contextVal context.Context

// TestIssue33 was a problem where metrics were not added to the default task object.
func TestIssue33(t *testing.T) {
	contextVal = context.Background()
	mp, err := initMeterProvider(t.Context(), "name")
	if err != nil {
		panic(err)
	}

	Service(InitArgs{Meta: Meta{Service: "test", Build: "v0.0.1"}}, WithMeterProvider(mp))

	tasks := context.Tasks(context.Background())

	meter := tasks.Meter()
	if fmt.Sprintf("%T", meter) == "noop.Meter" {
		t.Errorf("TestIssue33: expected meter to not be the noop.Meter")
	}
}

func initMeterProvider(ctx context.Context, serviceName string) (*sdkmetric.MeterProvider, error) {
	name := semconv.ServiceNameKey.String(serviceName)
	res, err := resource.New(ctx,
		resource.WithAttributes(
			// The service name used to display traces in backends
			name,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	metricExporter, err := otelprometheus.New(otelprometheus.WithRegisterer(prometheus.DefaultRegisterer))
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics exporter: %w", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(metricExporter),
		sdkmetric.WithResource(res),
	)

	if err := initMetrics(meterProvider.Meter(subsystem)); err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	return meterProvider, nil
}

func metricName(name string) string {
	return fmt.Sprintf("%s_%s", subsystem, name)
}

func initMetrics(meter metric.Meter) error {
	var err error
	runningCount, err = meter.Int64UpDownCounter(metricName("running_count"),
		metric.WithDescription("Number of running procedures"))
	if err != nil {
		return err
	}
	return nil
}
