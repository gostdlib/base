package sampler

import (
	"context"
	"testing"

	grpcContext "github.com/gostdlib/base/context/grpc"
	internalCtx "github.com/gostdlib/base/internal/context"
	grpcFilter "github.com/gostdlib/base/telemetry/otel/trace/sampler/filters/grpc"

	"github.com/kylelemons/godebug/pretty"
	"go.opentelemetry.io/otel/sdk/trace"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestAddFilters(t *testing.T) {
	t.Parallel()

	// We are only going to test the duplicate scenario as
	// other tests use AddFilters() for basic adding without issue.

	filters := []Filter{
		grpcFilter.Matcher{Op: "hello"}.MustCompile(),
		grpcFilter.Matcher{Op: "world"}.MustCompile(),
		grpcFilter.Matcher{Op: "sunshine"}.MustCompile(),
	}

	f, err := New(sdkTrace.NeverSample())
	if err != nil {
		panic(err)
	}

	f.ReplaceFilters(filters...)

	got := *(*f.filters).Load()
	if len(got) != len(filters) {
		t.Fatalf("TestAddFilters: did not get the expected number of filters, got %d", len(got))
	}

}

func TestReplaceFilters(t *testing.T) {
	t.Parallel()

	filtersA := []Filter{
		grpcFilter.Matcher{Op: "hello"}.MustCompile(),
		grpcFilter.Matcher{Op: "world"}.MustCompile(),
	}
	filtersB := []Filter{
		grpcFilter.Matcher{Op: "sunshine"}.MustCompile(),
		grpcFilter.Matcher{Op: "hello"}.MustCompile(),
	}

	f, err := New(sdkTrace.NeverSample())
	if err != nil {
		panic(err)
	}

	f.ReplaceFilters(filtersA...)
	f.ReplaceFilters(filtersB...)

	got := *(*f.filters).Load()
	if len(got) != len(filtersB) {
		t.Fatalf("TestReplaceFilters: did not get the right number of filters")
	}
	if diff := pretty.Compare(filtersB, got); diff != "" {
		t.Errorf("TestReplaceFilters: -want/+got:\n%s", diff)
	}
}

func TestShouldSample(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		filters   []Filter
		secondary sdkTrace.Sampler
		params    trace.SamplingParameters
		want      trace.SamplingResult
	}{
		{
			name: "context.Trace() == true",
			params: trace.SamplingParameters{
				ParentContext: internalCtx.SetShouldTrace(context.Background(), true),
			},
			want: sdkTrace.SamplingResult{
				Decision: sdkTrace.RecordAndSample,
			},
		},
		{
			name: "filter match",
			filters: []Filter{
				grpcFilter.Matcher{Op: ".*(hello).*"}.MustCompile(),
			},
			params: trace.SamplingParameters{
				ParentContext: grpcContext.SetMetadata(context.Background(), grpcContext.Metadata{Op: "blahelloabba"}),
			},
			want: sdkTrace.SamplingResult{
				Decision: sdkTrace.RecordAndSample,
			},
		},
		{
			name:      "secondary match",
			secondary: sdkTrace.AlwaysSample(),
			want: sdkTrace.SamplingResult{
				Decision: sdkTrace.RecordAndSample,
			},
		},
		{
			name: "no filter no secondary",
			want: sdkTrace.SamplingResult{
				Decision: sdkTrace.Drop,
			},
		},
	}

	for _, test := range tests {
		f, err := New(test.secondary)
		if err != nil {
			panic(err)
		}
		f.ReplaceFilters(test.filters...)

		got := f.ShouldSample(test.params)
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestShouldSample(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}
