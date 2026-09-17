package trace

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gostdlib/base/env/detect"
	"github.com/gostdlib/base/values/isset"
	"github.com/kylelemons/godebug/pretty"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

func TestIniter(t *testing.T) {
	// Sequential: Set() mutates the package-level default trace provider.

	prodEnv := detect.RunEnv{
		IsKubernetes:  true,
		IgnoreTesting: true,
	}
	if prodEnv.Prod() != true {
		panic("wtf")
	}
	nonProdEnv := detect.RunEnv{}

	prodProviderOk := func(ctx context.Context, endpoint string, sampleRate float64) (*sdkTrace.TracerProvider, error) {
		return localProvider(ctx, nil)
	}
	prodProviderErr := func(context.Context, string, float64) (*sdkTrace.TracerProvider, error) {
		return nil, errors.New("error")
	}

	localProviderOk := func(ctx context.Context, w io.Writer) (*sdkTrace.TracerProvider, error) {
		return localProvider(ctx, nil)
	}
	localProviderErr := func(ctx context.Context, w io.Writer) (*sdkTrace.TracerProvider, error) {
		return nil, errors.New("error")
	}

	tests := []struct {
		name          string
		env           detect.RunEnv
		endpoint      string
		disable       bool
		defaultTP     *sdkTrace.TracerProvider
		prodProvider  func(context.Context, string, float64) (*sdkTrace.TracerProvider, error)
		localProvider func(context.Context, io.Writer) (*sdkTrace.TracerProvider, error)

		wantErr bool
		// wantGlobals is true when the provider and propagator should be registered as the OTEL globals.
		wantGlobals bool
	}{
		{
			name:        "Success: a provider set before Init is registered as the global provider",
			defaultTP:   &sdkTrace.TracerProvider{},
			wantGlobals: true,
		},
		{
			name:      "Success: a provider set before Init is not registered when tracing is disabled",
			defaultTP: &sdkTrace.TracerProvider{},
			disable:   true,
		},
		{
			name: "Success: in prod with no endpoint set, no provider is created",
			env:  prodEnv,
		},
		{
			name:         "Error: in prod with an endpoint set, the prod provider fails",
			env:          prodEnv,
			endpoint:     "endpoint",
			prodProvider: prodProviderErr,
			wantErr:      true,
		},
		{
			name:         "Success: in prod with an endpoint set, the prod provider is registered",
			env:          prodEnv,
			endpoint:     "endpoint",
			prodProvider: prodProviderOk,
			wantGlobals:  true,
		},
		{
			name:         "Success: in prod with an endpoint set and tracing disabled, no provider is created",
			env:          prodEnv,
			endpoint:     "endpoint",
			disable:      true,
			prodProvider: prodProviderOk,
		},
		{
			name:    "Success: in non-prod with tracing disabled, no provider is created",
			env:     nonProdEnv,
			disable: true,
		},
		{
			name:          "Error: in non-prod, the local provider fails",
			env:           nonProdEnv,
			localProvider: localProviderErr,
			wantErr:       true,
		},
		{
			name:          "Success: in non-prod, the local provider is registered",
			env:           nonProdEnv,
			localProvider: localProviderOk,
			wantGlobals:   true,
		},
	}

	for _, test := range tests {
		var gotTP trace.TracerProvider
		setTrace := func(tp trace.TracerProvider) {
			gotTP = tp
		}
		var gotPropagator propagation.TextMapPropagator
		setPropagator := func(p propagation.TextMapPropagator) {
			gotPropagator = p
		}

		Set(test.defaultTP)

		i := initer{
			endpoint:         test.endpoint,
			env:              test.env,
			disable:          test.disable,
			prodProvider:     test.prodProvider,
			localProvider:    test.localProvider,
			setTraceProvider: setTrace,
			setPropagator:    setPropagator,
		}

		err := i.Init()
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestIniter(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestIniter(%s): got err != nil, want err == nil: %v", test.name, err)
			continue
		case err != nil:
			continue
		}

		if !test.wantGlobals {
			if gotTP != nil {
				t.Errorf("TestIniter(%s): got global tracer provider registered, want none", test.name)
			}
			if gotPropagator != nil {
				t.Errorf("TestIniter(%s): got global propagator registered, want none", test.name)
			}
			continue
		}

		// The registered global must be the provider Default() returns, whether it came from Set() or was created.
		if gotTP == nil || gotTP != trace.TracerProvider(Default()) {
			t.Errorf("TestIniter(%s): got global tracer provider %p, want Default() %p", test.name, gotTP, Default())
		}
		if _, ok := gotPropagator.(propagation.TraceContext); !ok {
			t.Errorf("TestIniter(%s): got global propagator %T, want propagation.TraceContext", test.name, gotPropagator)
		}
	}
}

func TestNewIniter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		sampleRate     isset.Float64
		wantSampleRate float64
	}{
		{
			name:           "Success: an unset sample rate uses the default rate",
			wantSampleRate: defaultSampleRate,
		},
		{
			name:           "Success: a sample rate set to zero stays zero",
			sampleRate:     isset.Float64{}.Set(0),
			wantSampleRate: 0,
		},
		{
			name:           "Success: a set sample rate is used as given",
			sampleRate:     isset.Float64{}.Set(0.5),
			wantSampleRate: 0.5,
		},
	}

	for _, test := range tests {
		i := newIniter("", false, test.sampleRate)
		if i.sampleRate != test.wantSampleRate {
			t.Errorf("TestNewIniter(%s): got sampleRate == %v, want sampleRate == %v", test.name, i.sampleRate, test.wantSampleRate)
		}
	}
}

func TestProdProvider(t *testing.T) {
	// Sequential: mutates the package-level connTimeout.
	ctx := context.Background()

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}

	grpcServer := grpc.NewServer()
	go grpcServer.Serve(lis)
	defer grpcServer.Stop()

	origTimeout := connTimeout
	t.Cleanup(
		func() {
			connTimeout = origTimeout
		},
	)
	connTimeout = 1 * time.Second

	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
	}{
		{
			name:    "empty endpoint",
			wantErr: true,
		},
		{
			name:     "with bad endpoint",
			endpoint: "localhost:4317",
			wantErr:  true,
		},
		{
			name:     "Success",
			endpoint: lis.Addr().String(),
		},
	}

	for _, test := range tests {
		_, err := prodProvider(ctx, test.endpoint, 0.1)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestProdProvider(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestProdProvider(%s): got err == %v, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
	}
}

type lockedBuilder struct {
	b  *strings.Builder
	mu sync.Mutex
}

func (b *lockedBuilder) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.b.Write(p)
}

func (b *lockedBuilder) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.b.String()
}

func TestLocalProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	buff := &lockedBuilder{b: &strings.Builder{}}

	tp, err := localProvider(context.Background(), buff)
	if err != nil {
		panic(err)
	}

	trace := tp.Tracer("TestLocalProvider")
	ctx, span := trace.Start(ctx, "TestLocalProviderSpan")
	span.AddEvent("testEvent")
	span.End()
	time.Sleep(2 * time.Second)

	if !strings.Contains(buff.String(), "testEvent") {
		t.Errorf("TestLocalProvider: cannot find our testEvent in stderr, got:\n%s", buff.String())
	}
}

func TestResources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want, err := resource.New(
		ctx,
		resource.WithTelemetrySDK(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
		resource.WithProcess(),
	)
	if err != nil {
		panic(err)
	}

	got, err := resources(ctx)
	if err != nil {
		t.Fatal("TestResources: error: ", err)
	}

	if diff := pretty.Compare(want, got); diff != "" {
		t.Errorf("TestResources: -want/+got:\n%s", diff)
	}
}
