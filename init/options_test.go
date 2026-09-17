package init

import (
	"testing"

	"github.com/gostdlib/base/values/isset"
	"github.com/kylelemons/godebug/pretty"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestResolveOptions(t *testing.T) {
	t.Parallel()

	tp := sdkTrace.NewTracerProvider()

	tests := []struct {
		name    string
		options []Option
		want    initOpts
		wantErr bool
	}{
		{
			name: "Success: no options leaves tracing at its defaults",
		},
		{
			name:    "Success: WithDisableTrace alone disables tracing",
			options: []Option{WithDisableTrace()},
			want:    initOpts{disableTrace: true},
		},
		{
			name:    "Success: WithTraceProvider alone sets the provider",
			options: []Option{WithTraceProvider(tp)},
			want:    initOpts{traceProvider: tp},
		},
		{
			name:    "Success: WithTraceSampleRate alone sets the rate",
			options: []Option{WithTraceSampleRate(0.5)},
			want:    initOpts{sampleRate: isset.Float64{}.Set(0.5)},
		},
		{
			name:    "Success: WithTraceSampleRate(0) sets a rate of zero",
			options: []Option{WithTraceSampleRate(0)},
			want:    initOpts{sampleRate: isset.Float64{}.Set(0)},
		},
		{
			name:    "Error: WithTraceProvider with a nil provider",
			options: []Option{WithTraceProvider(nil)},
			wantErr: true,
		},
		{
			name:    "Error: WithDisableTrace after WithTraceProvider",
			options: []Option{WithTraceProvider(tp), WithDisableTrace()},
			wantErr: true,
		},
		{
			name:    "Error: WithTraceProvider after WithDisableTrace",
			options: []Option{WithDisableTrace(), WithTraceProvider(tp)},
			wantErr: true,
		},
		{
			name:    "Error: WithDisableTrace after WithTraceSampleRate",
			options: []Option{WithTraceSampleRate(0.5), WithDisableTrace()},
			wantErr: true,
		},
		{
			name:    "Error: WithTraceSampleRate after WithDisableTrace",
			options: []Option{WithDisableTrace(), WithTraceSampleRate(0.5)},
			wantErr: true,
		},
		{
			name:    "Error: WithDisableTrace after WithTraceSampleRate(0)",
			options: []Option{WithTraceSampleRate(0), WithDisableTrace()},
			wantErr: true,
		},
		{
			name:    "Error: WithTraceProvider after WithTraceSampleRate",
			options: []Option{WithTraceSampleRate(0.5), WithTraceProvider(tp)},
			wantErr: true,
		},
		{
			name:    "Error: WithTraceSampleRate after WithTraceProvider",
			options: []Option{WithTraceProvider(tp), WithTraceSampleRate(0.5)},
			wantErr: true,
		},
		{
			name:    "Error: WithTraceProvider after WithTraceSampleRate(0)",
			options: []Option{WithTraceSampleRate(0), WithTraceProvider(tp)},
			wantErr: true,
		},
	}

	for _, test := range tests {
		got, err := resolveOptions(test.options)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestResolveOptions(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestResolveOptions(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestResolveOptions(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}
