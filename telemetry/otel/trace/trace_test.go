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
	"github.com/kylelemons/godebug/pretty"
	"go.opentelemetry.io/otel/sdk/resource"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

func TestIniter(t *testing.T) {
	t.Parallel()

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
		name              string
		env               detect.RunEnv
		endpoint          string
		localTraceDisable bool
		defaultTP         *sdkTrace.TracerProvider
		prodProvider      func(context.Context, string, float64) (*sdkTrace.TracerProvider, error)
		localProvider     func(context.Context, io.Writer) (*sdkTrace.TracerProvider, error)

		wantErr            bool
		wantSetTraceCalled bool
	}{
		{
			name:      "defaultTP is already set",
			defaultTP: &sdkTrace.TracerProvider{},
		},
		{
			name: "In prod, but no endpoint set",
			env:  prodEnv,
		},
		{
			name:         "In prod, endpoint set, but error",
			env:          prodEnv,
			endpoint:     "endpoint",
			prodProvider: prodProviderErr,
			wantErr:      true,
		},
		{
			name:               "In prod, endpoint set",
			env:                prodEnv,
			endpoint:           "endpoint",
			prodProvider:       prodProviderOk,
			wantSetTraceCalled: true,
		},
		{
			name:              "Non-prod, local trace disabled",
			env:               nonProdEnv,
			localTraceDisable: true,
		},
		{
			name:          "Non-prod, localProvider error",
			env:           nonProdEnv,
			localProvider: localProviderErr,
			wantErr:       true,
		},
		{
			name:               "Non-prod",
			env:                nonProdEnv,
			localProvider:      localProviderOk,
			wantSetTraceCalled: true,
		},
	}

	for _, test := range tests {
		setTraceCalled := false
		setTrace := func(tp trace.TracerProvider) {
			setTraceCalled = true
		}

		Set(test.defaultTP)

		i := initer{
			endpoint:          test.endpoint,
			env:               test.env,
			localTraceDisable: test.localTraceDisable,
			prodProvider:      test.prodProvider,
			localProvider:     test.localProvider,
			setTraceProvider:  setTrace,
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

		if setTraceCalled != test.wantSetTraceCalled {
			t.Errorf("TestIniter(%s): got setTraceCalled == %t, want setTraceCalled == %t", test.name, setTraceCalled, test.wantSetTraceCalled)
		}
	}
}

func TestProdProvider(t *testing.T) {
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
