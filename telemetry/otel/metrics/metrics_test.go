package metrics

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// TestIniterServe actually tests that the metrics server will start
// and contains expected content.
func TestIniterServe(t *testing.T) {
	const targetInfo = "testing"

	origDefault := defaultProvider
	t.Cleanup(func() { Set(origDefault) })
	origOtel := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(origOtel) })

	rsc, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String(string(semconv.ServiceNameKey), targetInfo),
		),
	)

	if err != nil {
		panic(err)
	}

	port, err := freePort()
	if err != nil {
		panic(err)
	}

	if err := initer(rsc, uint16(port)); err != nil {
		t.Fatalf("TestIniter(): failed to init metrics: %v", err)
	}
	defer Close()

	time.Sleep(1 * time.Second)
	hclient := http.Client{}
	resp, err := hclient.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Check HTTP response status
	if resp.StatusCode != http.StatusOK {
		t.Fatal("status code problem")
	}

	metrics := []string{}
	// Parse and print metrics
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}

		// Print or process each metric line

		metrics = append(metrics, line)
	}

	if err := scanner.Err(); err != nil {
		panic(err)
	}

	found := map[string]bool{
		"go_goroutines":          false,
		"go_memstats":            false,
		"cpu_time_seconds_total": false,
		"runtime":                false,
		fmt.Sprintf("target_info{service_name=%q}", targetInfo): false,
	}

	for _, metric := range metrics {
		for key := range found {
			if strings.Contains(metric, key) {
				found[key] = true
			}
		}
	}

	for key, value := range found {
		if !value {
			t.Errorf("metric not found: %s", key)
		}
	}
}

// TestIniterServeWithOtherProvider tests that when using a different provider,
// we don't see the "reader is not registered" log when scraping metrics which
// would indicate that there is an unused exporter. This log is noisy since it
// will show up every time the endpoint is scraped.
func TestIniterServeWithOtherProvider(t *testing.T) {
	const targetInfo = "testing"

	origDefault := defaultProvider
	t.Cleanup(func() { Set(origDefault) })
	origOtel := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(origOtel) })

	// Capture log output
	var logBuf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(origOutput) })

	rsc, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String(string(semconv.ServiceNameKey), targetInfo),
		),
	)
	if err != nil {
		panic(err)
	}

	// Set the provider to otherProvider instead of default
	Set(otherProvider{})

	port, err := freePort()
	if err != nil {
		panic(err)
	}

	if err := initer(rsc, uint16(port)); err != nil {
		t.Fatalf("TestIniterServeWithOtherProvider(): failed to init metrics: %v", err)
	}
	defer Close()

	time.Sleep(1 * time.Second)

	// Clear the log buffer before scraping
	logBuf.Reset()

	// Scrape the metrics endpoint - this is when the log would appear if we have an unused exporter
	// that is not registered.
	hclient := http.Client{}
	resp, err := hclient.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Check that we don't see the "reader is not registered" log
	logOutput := logBuf.String()
	if strings.Contains(logOutput, "reader is not registered") {
		t.Errorf("TestIniterServeWithOtherProvider: unexpected 'reader is not registered' log: %s", logOutput)
	}
}

type otherProvider struct {
	metric.MeterProvider
}

func (otherProvider) Meter(_ string, _ ...metric.MeterOption) metric.Meter {
	return noop.NewMeterProvider().Meter("other")
}

func mustNewResc(options ...resource.Option) *resource.Resource {
	r, err := resource.New(context.Background(), options...)
	if err != nil {
		panic(err)
	}
	return r
}

func TestIniter(t *testing.T) {
	orig := otel.GetMeterProvider()
	t.Cleanup(
		func() { otel.SetMeterProvider(orig) },
	)
	dpOrig := defaultProvider
	t.Cleanup(
		func() { defaultProvider = dpOrig },
	)

	promExporter, err := otelprometheus.New(otelprometheus.WithRegisterer(prometheus.DefaultRegisterer))
	if err != nil {
		panic(err)
	}

	tests := []struct {
		name            string
		defaultProvider metric.MeterProvider
		meta            *resource.Resource
		wantErr         bool
		wantProvider    metric.MeterProvider
	}{
		{
			name:            "no-op provider",
			defaultProvider: noop.NewMeterProvider(),
			wantProvider:    noop.NewMeterProvider(),
		},
		{
			name:            "meta is nil",
			defaultProvider: otherProvider{},
			wantErr:         true,
		},
		{
			name:            "meta attributes len is zero",
			meta:            &resource.Resource{},
			defaultProvider: otherProvider{},
			wantErr:         true,
		},
		{
			name: "meta attributes doesn't have a serviceKey",
			meta: mustNewResc(
				resource.WithAttributes(
					attribute.Bool("some key", true),
					attribute.String("another key", "yo"),
				),
			),
			defaultProvider: otherProvider{},
			wantErr:         true,
		},
		{
			name: "meta service key doesn't have a string value",
			meta: mustNewResc(
				resource.WithAttributes(
					attribute.Bool(string(semconv.ServiceNameKey), true),
				),
			),
			defaultProvider: otherProvider{},
			wantErr:         true,
		},
		{
			name: "meta service key has empty string value",
			meta: mustNewResc(
				resource.WithAttributes(
					attribute.String(string(semconv.ServiceNameKey), ""),
				),
			),
			defaultProvider: otherProvider{},
			wantErr:         true,
		},
		{
			name: "default provider already set",
			meta: mustNewResc(
				resource.WithAttributes(
					attribute.String(string(semconv.ServiceNameKey), "service"),
				),
			),
			defaultProvider: otherProvider{},
			wantProvider:    otherProvider{},
		},
		{
			name: "default provider not set",
			meta: mustNewResc(
				resource.WithAttributes(
					attribute.String(string(semconv.ServiceNameKey), "service"),
				),
			),
			defaultProvider: nil,
			wantProvider: sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(promExporter),
				sdkmetric.WithResource(
					mustNewResc(
						resource.WithAttributes(
							attribute.String(string(semconv.ServiceNameKey), "service"),
						),
					),
				),
			),
		},
	}

	for _, test := range tests {
		defaultProvider = test.defaultProvider

		err := initer(test.meta, 0)
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestIniter(%s): expected error, got nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestIniter(%s): unexpected error: %v", test.name, err)
			continue
		case err != nil:
			continue
		}

		got := fmt.Sprintf("%T", otel.GetMeterProvider())
		want := fmt.Sprintf("%T", test.wantProvider)
		if got != want {
			t.Errorf("TestIniter(%s): expected provider %s, got %s", test.name, want, got)
		}
	}
}

func freePort() (port int, err error) {
	var a *net.TCPAddr

	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}
