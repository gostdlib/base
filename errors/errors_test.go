package errors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"testing"
	"time"

	"github.com/gostdlib/base/telemetry/log"

	"github.com/go-json-experiment/json"
	"github.com/kylelemons/godebug/pretty"

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
			test.want.Filename = fn
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
				"Filename":   "",
				"Line":       0,
				"Request":    "<nil>",
				"CallID":     "callID",
				"Type":       "Unknown",
				"ErrTime":    time.Time{}.UTC(),
				"TraceID":    "",
				"level":      "ERROR",
				"msg":        "error message not provided",
			},
		},
		{
			name: "Error with everything and a proto req",
			e: Error{
				Category: CatReq,
				Filename: "filename",
				Line:     123,
				Type:     TypeBadRequest,
				ErrTime:  ti,
				Msg:      fmt.Errorf("something went wrong"),
			},
			req: &pb.Error{Id: "2"},
			want: map[string]any{
				"Category":   "Request",
				"CustomerID": "customerID",
				"Filename":   "filename",
				"Line":       123,
				"Request":    "{\"Id\":\"2\"}",
				"CallID":     "callID",
				"ErrTime":    ti.UTC(),
				"TraceID":    "",
				"Type":       "BadRequest",
				"level":      "ERROR",
				"msg":        "something went wrong",
			},
		},
		{
			name: "Error with everything and a http.Request",
			e: Error{
				Category: CatReq,
				Filename: "filename",
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
				"Filename":   "filename",
				"Line":       123,
				"Request":    "{\"Method\":\"GET\",\"URL\":{\"Scheme\":\"https\",\"Opaque\":\"\",\"User\":null,\"Host\":\"www.microsoft.com\",\"Path\":\"\",\"RawPath\":\"\",\"OmitHost\":false,\"ForceQuery\":false,\"RawQuery\":\"\",\"Fragment\":\"\",\"RawFragment\":\"\"},\"Proto\":\"\",\"ProtoMajor\":0,\"ProtoMinor\":0,\"Header\":{},\"Body\":null,\"GetBody\"",
				"CallID":     "callID",
				"ErrTime":    ti.UTC(),
				"TraceID":    "",
				"Type":       "BadRequest",
				"level":      "ERROR",
				"msg":        "something went wrong",
			},
		},
		{
			name: "Error has Msg that has sub-attributes",
			e: Error{
				Category: CatReq,
				Filename: "filename",
				Line:     123,
				Type:     TypeBadRequest,
				ErrTime:  ti,
				Msg:      SQLQueryErr{Query: "SELECT * FROM users", Msg: errors.New("something went wrong")},
			},
			ctx: ctxWithSpan,
			want: map[string]any{
				"Category":   "Request",
				"CustomerID": "customerID",
				"Filename":   "filename",
				"Line":       123,
				"Request":    "<nil>",
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
				Filename: "filename",
				Line:     123,
				Type:     TypeBadRequest,
				ErrTime:  ti,
				Msg:      ErrTopAttrs("hello"),
			},
			want: map[string]any{
				"Category":   "Request",
				"CustomerID": "customerID",
				"Filename":   "filename",
				"Line":       123,
				"Request":    "<nil>",
				"CallID":     "callID",
				"ErrTime":    ti.UTC(),
				"Type":       "BadRequest",
				"TraceID":    "",
				"level":      "ERROR",
				"msg":        "hello",
				"key":        "value",
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
