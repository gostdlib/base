package span

import (
	"context"
	"fmt"
	"log"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	baseTrace "github.com/gostdlib/base/telemetry/otel/trace"
	"github.com/kylelemons/godebug/pretty"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type event struct {
	name string
	opts []trace.EventOption
}

type fakeSpan struct {
	trace.Span
	sdkTrace.ReadOnlySpan

	events      []event
	status      sdkTrace.Status
	isRecording bool
	endCalled   bool
}

func (f *fakeSpan) AddEvent(name string, options ...trace.EventOption) {
	f.events = append(f.events, event{name: name, opts: options})
}

func (f *fakeSpan) IsRecording() bool {
	return f.isRecording
}

// Status implements Status from the ReadOnlySpan.
func (f *fakeSpan) Status() sdkTrace.Status {
	return f.status
}

// SetStatus implements SetStatus from the Span.
func (f *fakeSpan) SetStatus(code codes.Code, description string) {
	f.status = sdkTrace.Status{Code: code, Description: description}
}

func (f *fakeSpan) End(options ...trace.SpanEndOption) {
	f.endCalled = true
}

// SpanContext implements SpanContext to avoid a ambivalent selector between ReadOnlySpan and Span.
// It just returns a empty SpanContext.
func (f *fakeSpan) SpanContext() trace.SpanContext {
	return trace.SpanContext{}
}

func TestNewOptions(t *testing.T) {
	t.Parallel()

	b := strings.Builder{}
	for i := 0; i < 300; i++ {
		b.WriteString("a")
	}

	var tooLongName = b.String()

	var reducedName = "..." + tooLongName[0:252]
	if len(reducedName) != 255 {
		panic("you didn't do this right")
	}

	tests := []struct {
		name             string
		options          []Option
		expectNameOption bool
		want             spanOpts
	}{
		{
			name: "no options",
			want: spanOpts{
				name: "github.com/gostdlib/base/telemetry/otel/trace/span.TestNewOptions",
				startOptions: []trace.SpanStartOption{
					trace.WithSpanKind(trace.SpanKindInternal),
				},
			},
		},
		{
			name: "all options",
			options: []Option{
				WithSpanStartOption(trace.WithSpanKind(trace.SpanKindProducer)),
				WithSpanEndOption(trace.WithStackTrace(true)),
				WithName("name"),
			},
			want: spanOpts{
				name: "name",
				startOptions: []trace.SpanStartOption{
					trace.WithSpanKind(trace.SpanKindProducer),
				},
				endOptions: []trace.SpanEndOption{
					trace.WithStackTrace(true),
				},
			},
		},
		{
			name: "name > 255",
			options: []Option{
				WithName(tooLongName),
			},
			want: spanOpts{
				name: reducedName,
				startOptions: []trace.SpanStartOption{
					trace.WithSpanKind(trace.SpanKindInternal),
				},
			},
		},
	}

	for _, test := range tests {
		// Do not separate this 3 lines, as it will change the line number of the caller.
		_, filename, line, _ := runtime.Caller(0)
		line = line + 2
		got := newOptions(test.options...)

		test.want.startOptions = append(
			test.want.startOptions,
			trace.WithAttributes(
				attribute.String("filename", filename),
				attribute.Int("line", line),
			),
		)

		if diff := pretty.Compare(got, test.want); diff != "" {
			t.Errorf("TestNewOptions(%s): -got +want %s", test.name, diff)
		}

		if utf8.RuneCountInString(got.name) > 255 {
			t.Errorf("TestNewOptions(%s): name size: got %d, want 255", test.name, utf8.RuneCountInString(got.name))
		}
	}
}

func TestEvent(t *testing.T) {
	t.Parallel()

	t.Cleanup(func() { now = time.Now })
	now = func() time.Time { return time.Unix(0, 0) }

	attr := attribute.String("key", "value")

	tests := []struct {
		name         string
		internalSpan *fakeSpan
		callname     string
		attrs        []attribute.KeyValue

		wantEvent event
	}{
		{
			name:         "not recording",
			internalSpan: &fakeSpan{},
			callname:     "event",
		},
		{
			name:         "empty name",
			internalSpan: &fakeSpan{isRecording: true},
			attrs:        []attribute.KeyValue{attr},
		},
		{
			name:         "no options",
			internalSpan: &fakeSpan{isRecording: true},
			callname:     "event",
			wantEvent:    event{name: "event", opts: []trace.EventOption{trace.WithTimestamp(now())}},
		},
		{
			name:         "with attributes",
			internalSpan: &fakeSpan{isRecording: true},
			callname:     "event",
			attrs:        []attribute.KeyValue{attr},
			wantEvent:    event{name: "event", opts: []trace.EventOption{trace.WithTimestamp(now()), trace.WithAttributes(attr)}},
		},
	}

	for _, test := range tests {
		s := Span{Span: test.internalSpan}

		s.Event(test.callname, test.attrs...)

		if reflect.ValueOf(test.wantEvent).IsZero() {
			if len(test.internalSpan.events) != 0 {
				t.Errorf("TestEvent(%s): got %d events, want 0", test.name, len(test.internalSpan.events))
			}
			continue
		}
		if len(test.internalSpan.events) != 1 {
			t.Errorf("TestEvent(%s): got 0 events, want 1", test.name)
			continue
		}
		if diff := pretty.Compare(test.wantEvent, test.internalSpan.events[0]); diff != "" {
			t.Errorf("TestEvent(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestSpan_End_Status_IsRecording(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		internalSpan *fakeSpan
		wantCode     codes.Code
		wantEnd      bool
	}{
		{
			name:         "not recording",
			internalSpan: &fakeSpan{},
			wantCode:     codes.Unset,
		},
		{
			name: "set ok from unset",
			internalSpan: &fakeSpan{
				isRecording: true,
			},
			wantCode: codes.Ok,
			wantEnd:  true,
		},
		{
			name: "code was error, so don't make it ok",
			internalSpan: &fakeSpan{
				isRecording: true,
				status:      sdkTrace.Status{Code: codes.Error},
			},
			wantCode: codes.Error,
			wantEnd:  true,
		},
	}

	for _, test := range tests {
		s := Span{Span: test.internalSpan}

		s.End()

		gotCode := test.internalSpan.Status().Code
		if gotCode != test.wantCode {
			t.Errorf("TestSpanEnd(%s): Code: got %v, want %v", test.name, gotCode, test.wantCode)
		}

		gotEnd := test.internalSpan.endCalled
		if gotEnd != test.wantEnd {
			t.Errorf("TestSpanEnd(%s): End: got %v, want %v", test.name, gotEnd, test.wantEnd)
		}
	}
}

func TestGetTracer(t *testing.T) {
	t.Parallel()

	noopTracer := noop.NewTracerProvider().Tracer("noop")
	testTracer := localProvider().Tracer("test")

	tests := []struct {
		name       string
		ctx        context.Context
		wantTracer trace.Tracer
	}{
		{
			name:       "Context is nil",
			wantTracer: noopTracer,
		},
		{
			name:       "Context doesn't have the TracerKey",
			ctx:        context.Background(),
			wantTracer: noopTracer,
		},
		{
			name:       "Context has the wrong type in the TracerKey",
			ctx:        context.WithValue(context.Background(), baseTrace.TracerKey, struct{}{}),
			wantTracer: noopTracer,
		},
		{
			name:       "Success",
			ctx:        context.WithValue(context.Background(), baseTrace.TracerKey, testTracer),
			wantTracer: testTracer,
		},
	}

	for _, test := range tests {
		got := getTracer(test.ctx)

		log.Printf("%T == %T ?", got, test.wantTracer)
		if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", test.wantTracer) {
			t.Errorf("TestGetTracer(%s): got %T, want %T", test.name, got, test.wantTracer)
		}
	}
}

func TestAttributes(t *testing.T) {
	tests := []struct {
		name        string
		input       []attribute.KeyValue
		expectedLen int
		wantErr     bool
	}{
		{
			name: "Add valid key-value pair",
			input: []attribute.KeyValue{
				attribute.String("key", "value"),
			},
			expectedLen: 1,
		},
		{
			name: "Add attribute with empty key",
			input: []attribute.KeyValue{
				attribute.String("", "value"),
			},
			expectedLen: 0,
			wantErr:     true,
		},
		{
			name: "Add multiple attributes with one error",
			input: []attribute.KeyValue{
				attribute.String("key", "value"),
				attribute.String("", "value"),
			},
			expectedLen: 1,
			wantErr:     true,
		},
		{
			name:        "Add no attributes",
			input:       []attribute.KeyValue{},
			expectedLen: 0,
			wantErr:     false,
		},
	}

	attrs := &Attributes{}
	for _, test := range tests {
		attrs.Reset()

		for _, kv := range test.input {
			attrs.Add(kv)
		}

		if len(attrs.Attrs) != test.expectedLen {
			t.Errorf("expected %d attributes, got %d", test.expectedLen, len(attrs.Attrs))
		}

		switch {
		case test.wantErr && attrs.Err() == nil:
			t.Errorf("TestAttributes(%s): got err == nil, want err != nil", test.name)
		case !test.wantErr && attrs.Err() != nil:
			t.Errorf("TestAttributes(%s): got err == %v, want err == nil", test.name, attrs.Err())
		}
	}
}

func localProvider() trace.TracerProvider {
	exp, err := stdouttrace.New(
		stdouttrace.WithWriter(os.Stderr),
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		panic(err)
	}

	bsp := sdkTrace.NewBatchSpanProcessor(exp, sdkTrace.WithBatchTimeout(1*time.Second))
	tp := sdkTrace.NewTracerProvider(
		sdkTrace.WithSpanProcessor(bsp),
	)
	return tp
}
