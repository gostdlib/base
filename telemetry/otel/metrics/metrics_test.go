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

	origDefault := loadDefault()
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

	if err := initer(rsc, uint16(port), defaultRegistry()); err != nil {
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

	origDefault := loadDefault()
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

	if err := initer(rsc, uint16(port), defaultRegistry()); err != nil {
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
	// Not parallel: initer and Close mutate package-level state (the otel global provider, defaultProvider and app).
	orig := otel.GetMeterProvider()
	t.Cleanup(
		func() { otel.SetMeterProvider(orig) },
	)
	dpOrig := loadDefault()
	t.Cleanup(
		func() { defaultProvider.Store(&provider{p: dpOrig}) },
	)
	// Registered after the restores so it runs first (cleanups are LIFO), while Default() is still the provider
	// this test installed. It covers a t.Fatal mid-loop; each case that starts a server also closes it below.
	t.Cleanup(func() { closeWithin(t, 10*time.Second) })

	serviceMeta := mustNewResc(
		resource.WithAttributes(
			attribute.String(string(semconv.ServiceNameKey), "service"),
		),
	)

	tests := []struct {
		name            string
		defaultProvider metric.MeterProvider
		meta            *resource.Resource
		wantErr         bool
		wantServe       bool
		wantProvider    metric.MeterProvider
	}{
		{
			name:            "Success: a no-op default provider is installed and no server starts",
			defaultProvider: noop.NewMeterProvider(),
			wantProvider:    noop.NewMeterProvider(),
		},
		{
			name:            "Error: a nil resource is rejected",
			defaultProvider: otherProvider{},
			wantErr:         true,
		},
		{
			name:            "Error: a resource with no attributes is rejected",
			meta:            &resource.Resource{},
			defaultProvider: otherProvider{},
			wantErr:         true,
		},
		{
			name: "Error: a resource without a service name is rejected",
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
			name: "Error: a service name that is not a string is rejected",
			meta: mustNewResc(
				resource.WithAttributes(
					attribute.Bool(string(semconv.ServiceNameKey), true),
				),
			),
			defaultProvider: otherProvider{},
			wantErr:         true,
		},
		{
			name: "Error: an empty service name is rejected",
			meta: mustNewResc(
				resource.WithAttributes(
					attribute.String(string(semconv.ServiceNameKey), ""),
				),
			),
			defaultProvider: otherProvider{},
			wantErr:         true,
		},
		{
			name:            "Success: an already set default provider is kept and served",
			meta:            serviceMeta,
			defaultProvider: otherProvider{},
			wantServe:       true,
			wantProvider:    otherProvider{},
		},
		{
			name:            "Success: with no default provider a Prometheus SDK provider is created and served",
			meta:            serviceMeta,
			defaultProvider: nil,
			wantServe:       true,
			wantProvider:    (*sdkmetric.MeterProvider)(nil),
		},
	}

	for _, test := range tests {
		defaultProvider.Store(&provider{p: test.defaultProvider})

		port, err := freePort()
		if err != nil {
			t.Fatalf("TestIniter(%s): freePort(): %v", test.name, err)
		}

		// Each case gets its own registry so nothing lands in prometheus.DefaultRegisterer.
		reg := prometheus.NewRegistry()
		err = initer(test.meta, uint16(port), reg)
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestIniter(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestIniter(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		got := fmt.Sprintf("%T", otel.GetMeterProvider())
		want := fmt.Sprintf("%T", test.wantProvider)
		if got != want {
			t.Errorf("TestIniter(%s): got provider %s, want provider %s", test.name, got, want)
		}

		if !test.wantServe {
			continue
		}
		// Wait for the server before closing it: closing before Listen() would leave it running.
		status, err := waitServing(t, port, 10*time.Second)
		closeWithin(t, 10*time.Second)
		if err != nil {
			t.Errorf("TestIniter(%s): metrics server never answered: %v", test.name, err)
			continue
		}
		if status != http.StatusOK {
			t.Errorf("TestIniter(%s): got /metrics status %d, want status %d", test.name, status, http.StatusOK)
		}
	}
}

// waitServing polls GET /metrics on port until it answers or d passes, and returns the status of the first answer.
func waitServing(t *testing.T, port int, d time.Duration) (int, error) {
	t.Helper()

	hclient := http.Client{Timeout: time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/metrics", port)
	deadline := time.Now().Add(d)
	for {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			return 0, err
		}
		resp, err := hclient.Do(req)
		if err == nil {
			resp.Body.Close()
			return resp.StatusCode, nil
		}
		if time.Now().After(deadline) {
			return 0, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// closeWithin calls Close() and fails the test if it has not returned within d.
func closeWithin(t *testing.T, d time.Duration) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		Close()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Errorf("%s: Close() did not return within %v", t.Name(), d)
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

func TestMeterName(t *testing.T) {
	tests := []struct {
		name       string
		stackFrame int
		want       string
	}{
		{
			name:       "Success: stackFrame 1 returns direct caller",
			stackFrame: 1,
			want:       "github.com/gostdlib/base/telemetry/otel/metrics",
		},
		{
			name:       "Success: stackFrame 2 returns caller's caller",
			stackFrame: 2,
			want:       "testing",
		},
	}

	for _, test := range tests {
		got := MeterName(test.stackFrame)
		if got != test.want {
			t.Errorf("TestMeterName(%s): got %q, want %q", test.name, got, test.want)
		}
	}
}

// meterNameHelper calls MeterName and is used to test that stackFrame correctly
// skips the right number of frames.
func meterNameHelper(stackFrame int) string {
	return MeterName(stackFrame)
}

func TestMeterNameWithHelper(t *testing.T) {
	tests := []struct {
		name       string
		stackFrame int
		want       string
	}{
		{
			name:       "Success: stackFrame 1 returns helper function's package",
			stackFrame: 1,
			want:       "github.com/gostdlib/base/telemetry/otel/metrics",
		},
		{
			name:       "Success: stackFrame 2 returns test function's package",
			stackFrame: 2,
			want:       "github.com/gostdlib/base/telemetry/otel/metrics",
		},
		{
			name:       "Success: stackFrame 3 returns testing package",
			stackFrame: 3,
			want:       "testing",
		},
	}

	for _, test := range tests {
		got := meterNameHelper(test.stackFrame)
		if got != test.want {
			t.Errorf("TestMeterNameWithHelper(%s): got %q, want %q", test.name, got, test.want)
		}
	}
}

func TestNewApp(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_new_app_total", Help: "TestNewApp counter."})
	reg.MustRegister(counter)
	counter.Inc()

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("TestNewApp: net.Listen(): %v", err)
	}

	a := newApp(reg)
	served := make(chan error, 1)
	go func() { served <- a.Listener(ln, listenConfig) }()
	t.Cleanup(
		func() {
			if err := a.Shutdown(); err != nil {
				t.Errorf("TestNewApp: Shutdown(): %v", err)
			}
			select {
			case err := <-served:
				if err != nil {
					t.Errorf("TestNewApp: Listener(): %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Errorf("TestNewApp: Listener() did not return after Shutdown()")
			}
		},
	)

	base := "http://" + ln.Addr().String()
	hclient := http.Client{Timeout: 5 * time.Second}

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "Success: the metrics route serves the gatherer's metrics",
			path:       "/metrics",
			wantStatus: http.StatusOK,
		},
		{
			name:       "Error: an unknown route returns not found",
			path:       "/unknown",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, test := range tests {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+test.path, nil)
		if err != nil {
			t.Fatalf("TestNewApp(%s): NewRequest: %v", test.name, err)
		}
		resp, err := hclient.Do(req)
		if err != nil {
			t.Errorf("TestNewApp(%s): got err == %s, want err == nil", test.name, err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != test.wantStatus {
			t.Errorf("TestNewApp(%s): got status %d, want status %d", test.name, resp.StatusCode, test.wantStatus)
		}
	}
}
