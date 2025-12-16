package errors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"path"
	"runtime"
	"testing"
	"time"

	goctx "github.com/gostdlib/base/context"
	"github.com/gostdlib/base/telemetry/log"
	"github.com/gostdlib/base/telemetry/otel/trace/span"

	"github.com/go-json-experiment/json"
	"github.com/kylelemons/godebug/pretty"

	"go.opentelemetry.io/otel/attribute"
	otelTrace "go.opentelemetry.io/otel/trace"

	pb "github.com/gostdlib/base/errors/example/proto"
)

type TestCat uint8

const (
	UnknownCat TestCat = 0
	CatReq     TestCat = 1
)

func (t TestCat) Category() string {
	if t == 0 {
		return "Unknown"
	}
	if t == 1 {
		return "Request"
	}
	return ""
}

type TestType uint8

const (
	UnknownType    TestType = 0
	TypeBadRequest TestType = 1
)

func (t TestType) Type() string {
	if t == 0 {
		return "Unknown"
	}
	if t == 1 {
		return "BadRequest"
	}
	return ""
}

var _ LogAttrer = SQLQueryErr{}

type SQLQueryErr struct {
	// Query is the SQL query that was being executed.
	Query string
	// Msg is the error message from the SQL query.
	Msg error
}

// Error returns the error message.
func (s SQLQueryErr) Error() string {
	return s.Msg.Error()
}

// Is returns true if the target is an SQLQueryErr type regardless of the Query or Msg.
func (s SQLQueryErr) Is(target error) bool {
	if _, ok := target.(SQLQueryErr); ok {
		return true
	}
	return false
}

// Unwrap unwraps the error.
func (s SQLQueryErr) Unwrap() error {
	return s.Msg
}

// LogAttrs implements the LogAttrer.LogAttrs() interface.
func (s SQLQueryErr) LogAttrs(context.Context) []slog.Attr {
	// You will notice here that I group the attributes with a category that includes the package path.
	// This is to prevent attribute name collisions with other packages.
	return []slog.Attr{slog.Group("package/path.SQLQueryErr", "Query", s.Query)}
}

var _ LogAttrer = ErrTopAttrs("")

// ErrTopAttrs is an example of a custom error where Attrs stores its attributes at the top level.
type ErrTopAttrs string

func (e ErrTopAttrs) Error() string {
	return string(e)
}

func (e ErrTopAttrs) LogAttrs(context.Context) []slog.Attr {
	return []slog.Attr{slog.Any("key", "value")}
}

func TestE(t *testing.T) {
	// Do not t.Parallel()

	ti := time.Now()
	t.Cleanup(func() { now = time.Now })
	now = func() time.Time {
		return ti
	}

	tests := []struct {
		name string
		msg  error
		want Error
	}{
		{
			name: "msg is nil",
			want: Error{
				Category: CatReq,
				Type:     TypeBadRequest,
				ErrTime:  ti.UTC(),
				Msg:      errors.New("bug: nil error"),
			},
		},
		{
			name: "msg is an Error(our error type)",
			msg: Error{
				Msg: errors.New("whatever"),
			},
			want: Error{
				Msg: errors.New("whatever"),
			},
		},
		{
			name: "msg is just some error(normal error type)",
			msg:  errors.New("error"),
			want: Error{
				Category: CatReq,
				Type:     TypeBadRequest,
				ErrTime:  ti.UTC(),
				Msg:      errors.New("error"),
			},
		},
	}

	for _, test := range tests {
		got := E(context.Background(), CatReq, TypeBadRequest, test.msg)
		_, fn, line, _ := runtime.Caller(0)
		if _, ok := test.msg.(Error); !ok {
			test.want.Line = line - 1
			test.want.File = fn
		}

		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestE(%s): -want/+got: %s\n", test.name, diff)
		}
	}
}

func TestError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  Error
		want string
	}{
		{
			name: "Get the error string from Msg",
			err:  Error{Msg: errors.New("error")},
			want: "error",
		},
		{
			name: "Get the error string from the override",
			err:  Error{Msg: errors.New("error"), MsgOverride: "override"},
			want: "override",
		},
	}

	for _, test := range tests {
		if test.want != test.err.Error() {
			t.Errorf("TestError(%s): got %q, want %q", test.name, test.err.Error(), test.want)
		}
	}
}

func TestIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    Error
		target error
		want   bool
	}{
		{
			name:   "Empty errors",
			err:    Error{},
			target: Error{},
			want:   true,
		},
		{
			name: "Error and nil",
			err:  Error{},
		},
		{
			name:   "Error and not an Error",
			err:    Error{},
			target: errors.New("error"),
		},
		{
			name:   "Errors don't match category",
			err:    Error{},
			target: Error{Category: CatReq},
		},
		{
			name:   "Errors don't match type",
			err:    Error{Category: CatReq},
			target: Error{Category: CatReq, Type: TypeBadRequest},
		},
		{
			name:   "Errors cat and type match",
			err:    Error{Category: CatReq, Type: TypeBadRequest},
			target: Error{Category: CatReq, Type: TypeBadRequest},
			want:   true,
		},
		{
			name:   "Empty Error target matches any Error",
			err:    Error{Category: CatReq, Type: TypeBadRequest},
			target: Error{},
			want:   true,
		},
		{
			name:   "Errors dont' have same Msg (still should match)",
			err:    Error{Category: CatReq, Type: TypeBadRequest, Msg: errors.New("error")},
			target: Error{Category: CatReq, Type: TypeBadRequest, Msg: errors.New("another error")},
			want:   true,
		},
	}

	for _, test := range tests {
		got := errors.Is(test.err, test.target)
		if got != test.want {
			t.Errorf("TestIs(%s): got %v, want %v", test.name, got, test.want)
		}
	}
}

func TestUnwrap(t *testing.T) {
	t.Parallel()

	e := Error{}

	if errors.Unwrap(e) != nil {
		t.Fatal("TestUnwrap: Unwrap() should return nil if .Msg is nil")
	}
	e.Msg = fmt.Errorf("error")

	if errors.Unwrap(e).Error() != "error" {
		t.Fatal("TestUnwrap: Unwrap does not unwrap the error")
	}
}

type fakeSpan struct {
	otelTrace.Span
	isRecording bool
	spanContext otelTrace.SpanContext
}

func (f fakeSpan) IsRecording() bool {
	return f.isRecording
}

func (f fakeSpan) SpanContext() otelTrace.SpanContext {
	return f.spanContext
}

func TestStackTrace(t *testing.T) {
	// Do not do t.Parallel().

	tests := []struct {
		name      string
		withTrace bool
	}{
		{
			name:      "with stack trace",
			withTrace: true,
		},
		{
			name: "without stack trace",
		},
	}

	for _, test := range tests {
		opts := []EOption{}
		if test.withTrace {
			opts = append(opts, WithStackTrace())
		}
		e := E(context.Background(), CatReq, TypeBadRequest, fmt.Errorf("something went wrong"), opts...)
		if test.withTrace && e.StackTrace == "" {
			t.Errorf("TestStackTrace(%s): got no stack trace, want stack trace", test.name)
		}
		if !test.withTrace && e.StackTrace != "" {
			t.Errorf("TestStackTrace(%s): got stack trace, want no stack trace", test.name)
		}
	}
}

func TestLog(t *testing.T) {
	// Do not do t.Parallel().

	ti := time.Now()
	t.Cleanup(func() { now = time.Now })
	now = func() time.Time {
		return ti
	}

	buff := &bytes.Buffer{}

	log.Set(slog.New(slog.NewJSONHandler(buff, nil)))

	tID := otelTrace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	span := fakeSpan{
		isRecording: true,
		spanContext: otelTrace.SpanContext{}.WithTraceID(tID),
	}

	ctxWithSpan := otelTrace.ContextWithSpan(context.Background(), span)

	tests := []struct {
		name       string
		e          Error
		callID     string
		customerID string
		req        any
		ctx        context.Context
		want       map[string]any
	}{
		{
			name: "Empty error with no req",
			want: map[string]any{
				"Category":   "Unknown",
				"CustomerID": "customerID",
				"ErrSrc":     "",
				"ErrLine":    0,
				"CallID":     "callID",
				"Type":       "Unknown",
				"ErrTime":    time.Time{}.UTC(),
				"level":      "ERROR",
				"msg":        "error message not provided",
			},
		},
		{
			name: "Error with everything and a proto req",
			e: Error{
				Category: CatReq,
				File:     "f  ilename",
				Line:     123,
				Type:     TypeBadRequest,
				ErrTime:  ti,
				Msg:      fmt.Errorf("something went wrong"),
			},
			req: &pb.Error{Id: "2"},
			want: map[string]any{
				"Category":   "Request",
				"CustomerID": "customerID",
				"ErrSrc":     "f  ilename",
				"ErrLine":    123,
				"Request":    "{\"Id\":\"2\"}",
				"CallID":     "callID",
				"ErrTime":    ti.UTC(),
				"Type":       "BadRequest",
				"level":      "ERROR",
				"msg":        "something went wrong",
			},
		},
		{
			name: "Error with everything and a http.Request",
			e: Error{
				Category: CatReq,
				File:     "f  ilename",
				Line:     123,
				Type:     TypeBadRequest,
				ErrTime:  ti,
				Msg:      fmt.Errorf("something went wrong"),
			},
			req: &http.Request{
				Method: "GET",
				URL:    urlMustParse("https://www.microsoft.com"),
			},
			want: map[string]any{
				"Category":   "Request",
				"CustomerID": "customerID",
				"ErrSrc":     "f  ilename",
				"ErrLine":    123,
				"Request":    "{\"method\":\"GET\",\"url\":\"https://www.microsoft.com\",\"header\":{},\"content_length\":0,\"host\":\"\",\"remote_addr\":\"\",\"request_uri\":\"\",\"body\":\"\"}",
				"CallID":     "callID",
				"ErrTime":    ti.UTC(),
				"Type":       "BadRequest",
				"level":      "ERROR",
				"msg":        "something went wrong",
			},
		},
		{
			name: "Error with everything but a request and a stack trace",
			e: Error{
				Category:   CatReq,
				File:       "filename",
				Line:       123,
				Type:       TypeBadRequest,
				ErrTime:    ti,
				Msg:        fmt.Errorf("something went wrong"),
				StackTrace: "stack trace",
			},
			want: map[string]any{
				"Category":   "Request",
				"CustomerID": "customerID",
				"ErrSrc":     "filename",
				"ErrLine":    123,
				"CallID":     "callID",
				"ErrTime":    ti.UTC(),
				"Type":       "BadRequest",
				"StackTrace": "stack trace",
				"level":      "ERROR",
				"msg":        "something went wrong",
			},
		},
		{
			name: "Error has Msg that has sub-attributes",
			e: Error{
				Category: CatReq,
				File:     "filename",
				Line:     123,
				Type:     TypeBadRequest,
				ErrTime:  ti,
				Msg:      SQLQueryErr{Query: "SELECT * FROM users", Msg: errors.New("something went wrong")},
			},
			ctx: ctxWithSpan,
			want: map[string]any{
				"Category":   "Request",
				"CustomerID": "customerID",
				"ErrSrc":     "filename",
				"ErrLine":    123,
				"CallID":     "callID",
				"ErrTime":    ti.UTC(),
				"Type":       "BadRequest",
				"level":      "ERROR",
				"TraceID":    tID.String(),
				"msg":        "something went wrong",
				"package/path.SQLQueryErr": map[string]any{
					"Query": "SELECT * FROM users",
				},
			},
		},
		{
			name: "Error has top level attributes",
			e: Error{
				Category: CatReq,
				File:     "filename",
				Line:     123,
				Type:     TypeBadRequest,
				ErrTime:  ti,
				Msg:      ErrTopAttrs("hello"),
			},
			want: map[string]any{
				"Category":   "Request",
				"CustomerID": "customerID",
				"ErrSrc":     "filename",
				"ErrLine":    123,
				"CallID":     "callID",
				"ErrTime":    ti.UTC(),
				"Type":       "BadRequest",
				"level":      "ERROR",
				"msg":        "hello",
				"key":        "value",
			},
		},
		{
			name: "Error created with context that has attributes",
			e: E(
				goctx.AddAttrs(context.Background(), slog.String("contextKey1", "contextValue1"), slog.Int("contextKey2", 42)),
				CatReq,
				TypeBadRequest,
				fmt.Errorf("something went wrong"),
			),
			want: map[string]any{
				"Category":    "Request",
				"CustomerID":  "customerID",
				"ErrSrc":      "/Users/blah/trees/github.com/gostdlib/base/errors/errors_test.go",
				"ErrLine":     489,
				"CallID":      "callID",
				"ErrTime":     ti.UTC(),
				"Type":        "BadRequest",
				"level":       "ERROR",
				"msg":         "something went wrong",
				"contextKey1": "contextValue1",
				"contextKey2": 42,
			},
		},
		{
			name: "Success: Error created with WithAttrs",
			e: E(
				context.Background(),
				CatReq,
				TypeBadRequest,
				fmt.Errorf("something went wrong"),
				WithAttrs(slog.String("customAttrKey", "customAttrValue"), slog.Int("customAttrNum", 99)),
			),
			want: map[string]any{
				"Category":       "Request",
				"CustomerID":     "customerID",
				"ErrSrc":         "/Users/blah/trees/github.com/gostdlib/base/errors/errors_test.go",
				"ErrLine":        511,
				"CallID":         "callID",
				"ErrTime":        ti.UTC(),
				"Type":           "BadRequest",
				"level":          "ERROR",
				"msg":            "something went wrong",
				"customAttrKey":  "customAttrValue",
				"customAttrNum":  99,
			},
		},
	}

	for _, test := range tests {
		buff.Reset()

		got := map[string]any{}
		if test.ctx == nil {
			test.ctx = context.Background()
		}

		test.e.Log(test.ctx, "callID", "customerID", test.req)

		if err := json.Unmarshal(buff.Bytes(), &got); err != nil {
			t.Logf("got: %s", buff.Bytes())
			t.Fatalf("TestLog(%s): got error on json.Unmarshal: %s", test.name, err)
		}
		if got["ErrTime"] != nil {
			errTimeStr := got["ErrTime"].(string)
			if errTimeStr != "" {
				errTime, err := time.Parse(time.RFC3339, errTimeStr)
				if err != nil {
					panic(err)
				}
				if !errTime.UTC().Equal(test.want["ErrTime"].(time.Time)) {
					t.Errorf("TestLog(%s): ErrTime: got %s, want %s", test.name, errTime, test.want["ErrTime"])
				}
				delete(got, "ErrTime")
				delete(test.want, "ErrTime")
			}
		}
		delete(got, "time") // This is the logging library time notation, which we don't control.
		test.want["ErrSrc"] = path.Base(test.want["ErrSrc"].(string))
		got["ErrSrc"] = path.Base(got["ErrSrc"].(string))
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestLog(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func urlMustParse(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestTraceAttrs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     Error
		prepend string
		attrs   span.Attributes
		want    []attribute.KeyValue
	}{
		{
			name: "Success: Error with valid Category and Type",
			err: Error{
				Category: CatReq,
				Type:     TypeBadRequest,
				File:     "test.go",
				Line:     42,
			},
			want: []attribute.KeyValue{
				attribute.String("Category", "Request"),
				attribute.String("Type", "BadRequest"),
				attribute.String("ErrSrc", "test.go"),
				attribute.Int("ErrLine", 42),
			},
		},
		{
			name: "Success: Error with Unknown Category",
			err: Error{
				Category: UnknownCat,
				Type:     TypeBadRequest,
				File:     "test.go",
				Line:     10,
			},
			want: []attribute.KeyValue{
				attribute.String("Category", "Unknown"),
				attribute.String("Type", "BadRequest"),
				attribute.String("ErrSrc", "test.go"),
				attribute.Int("ErrLine", 10),
			},
		},
		{
			name: "Success: Error with Unknown Type",
			err: Error{
				Category: CatReq,
				Type:     UnknownType,
				File:     "test.go",
				Line:     10,
			},
			want: []attribute.KeyValue{
				attribute.String("Category", "Request"),
				attribute.String("Type", "Unknown"),
				attribute.String("ErrSrc", "test.go"),
				attribute.Int("ErrLine", 10),
			},
		},
		{
			name: "Success: Error with slog attrs",
			err: Error{
				Category: CatReq,
				Type:     TypeBadRequest,
				File:     "test.go",
				Line:     1,
				attrs:    []slog.Attr{slog.String("customKey", "customValue")},
			},
			want: []attribute.KeyValue{
				attribute.String("Category", "Request"),
				attribute.String("Type", "BadRequest"),
				attribute.String("ErrSrc", "test.go"),
				attribute.Int("ErrLine", 1),
				attribute.String(".customKey", "customValue"),
			},
		},
		{
			name: "Success: Error with prepend namespace",
			err: Error{
				Category: CatReq,
				Type:     TypeBadRequest,
				File:     "test.go",
				Line:     1,
				attrs:    []slog.Attr{slog.String("key", "value")},
			},
			prepend: "mypackage",
			want: []attribute.KeyValue{
				attribute.String("Category", "Request"),
				attribute.String("Type", "BadRequest"),
				attribute.String("ErrSrc", "test.go"),
				attribute.Int("ErrLine", 1),
				attribute.String("mypackage.key", "value"),
			},
		},
		{
			name: "Success: Error with existing attrs in span.Attributes",
			err: Error{
				Category: CatReq,
				Type:     TypeBadRequest,
				File:     "test.go",
				Line:     1,
			},
			attrs: span.Attributes{
				Attrs: []attribute.KeyValue{attribute.String("existing", "attr")},
			},
			want: []attribute.KeyValue{
				attribute.String("existing", "attr"),
				attribute.String("Category", "Request"),
				attribute.String("Type", "BadRequest"),
				attribute.String("ErrSrc", "test.go"),
				attribute.Int("ErrLine", 1),
			},
		},
		{
			name: "Success: attrs.Err() returns early",
			err: Error{
				Category: CatReq,
				Type:     TypeBadRequest,
				File:     "test.go",
				Line:     1,
			},
			attrs: func() span.Attributes {
				a := span.Attributes{}
				a.Add(attribute.KeyValue{Key: "", Value: attribute.StringValue("bad")})
				return a
			}(),
			want: nil,
		},
	}

	for _, test := range tests {
		got := test.err.TraceAttrs(context.Background(), test.prepend, test.attrs)
		if diff := pretty.Compare(test.want, got.Attrs); diff != "" {
			t.Errorf("TestTraceAttrs(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestSlogAttrsToOtelAttrs(t *testing.T) {
	t.Parallel()

	testTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name      string
		namespace []string
		attrs     []slog.Attr
		want      []attribute.KeyValue
	}{
		{
			name:      "Success: Bool conversion",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{slog.Bool("enabled", true)},
			want:      []attribute.KeyValue{attribute.Bool("ns.enabled", true)},
		},
		{
			name:      "Success: Int64 conversion",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{slog.Int64("count", 42)},
			want:      []attribute.KeyValue{attribute.Int64("ns.count", 42)},
		},
		{
			name:      "Success: Uint64 within int64 range",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{slog.Uint64("count", 100)},
			want:      []attribute.KeyValue{attribute.Int64("ns.count", 100)},
		},
		{
			name:      "Success: Uint64 exceeding int64 range returns nothing",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{slog.Uint64("big", math.MaxUint64)},
			want:      []attribute.KeyValue{},
		},
		{
			name:      "Success: Float64 conversion",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{slog.Float64("rate", 3.14)},
			want:      []attribute.KeyValue{attribute.Float64("ns.rate", 3.14)},
		},
		{
			name:      "Success: String conversion",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{slog.String("name", "test")},
			want:      []attribute.KeyValue{attribute.String("ns.name", "test")},
		},
		{
			name:      "Success: Duration conversion adds _ns suffix",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{slog.Duration("elapsed", 5*time.Second)},
			want: []attribute.KeyValue{
				{Key: "ns.elapsed_ns", Value: attribute.Int64Value(int64(5 * time.Second))},
			},
		},
		{
			name:      "Success: Duration key already has ns suffix",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{slog.Duration("elapsed_ns", 5*time.Second)},
			want: []attribute.KeyValue{
				{Key: "ns.elapsed_ns", Value: attribute.Int64Value(int64(5 * time.Second))},
			},
		},
		{
			name:      "Success: Time conversion adds _unix_ns suffix",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{slog.Time("created", testTime)},
			want: []attribute.KeyValue{
				{Key: "ns.created_unix_ns", Value: attribute.Int64Value(testTime.UnixNano())},
			},
		},
		{
			name:      "Success: Time key already has unix_ns suffix",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{slog.Time("created_unix_ns", testTime)},
			want: []attribute.KeyValue{
				{Key: "ns.created_unix_ns", Value: attribute.Int64Value(testTime.UnixNano())},
			},
		},
		{
			name:      "Success: Group with nested attributes",
			namespace: []string{"ns"},
			attrs: []slog.Attr{
				slog.Group("user", slog.String("name", "john"), slog.Int64("age", 30)),
			},
			want: []attribute.KeyValue{
				attribute.String("ns.user.name", "john"),
				attribute.Int64("ns.user.age", 30),
			},
		},
		{
			name:      "Success: KindAny returns nil",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{slog.Any("data", struct{ X int }{X: 1})},
			want:      []attribute.KeyValue{},
		},
		{
			name:      "Success: Multiple attrs in single call",
			namespace: []string{"ns"},
			attrs: []slog.Attr{
				slog.String("a", "1"),
				slog.Int64("b", 2),
				slog.Bool("c", true),
			},
			want: []attribute.KeyValue{
				attribute.String("ns.a", "1"),
				attribute.Int64("ns.b", 2),
				attribute.Bool("ns.c", true),
			},
		},
		{
			name:      "Success: Empty attrs slice",
			namespace: []string{"ns"},
			attrs:     []slog.Attr{},
			want:      []attribute.KeyValue{},
		},
		{
			name:      "Success: Empty namespace",
			namespace: []string{},
			attrs:     []slog.Attr{slog.String("key", "value")},
			want:      []attribute.KeyValue{attribute.String("key", "value")},
		},
	}

	for _, test := range tests {
		got := slogAttrsToOtelAttrs(test.namespace, test.attrs)
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestSlogAttrsToOtelAttrs(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}
