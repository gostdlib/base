/*
Package errors provides an error type suitable for all errors returned by packages used to build services using
the base/ set of packages. This error type can be used to automatically handle error logging and RPC error returns.

This package should be used to build a service specific error package and not used directly.

Services should create their own errors packages. This can be achieved for new projects using
the genproject tool. Otherwise, you can copy the following and fill it in. Remember that
you must use "go generate" for everything to work.

	package errors

	import (
	    "github.com/gostdlib/base/context"
		"github.com/gostdlib/base/errors"
	)

	//go:generate stringer -type=Category -linecomment

	// Category represents the category of the error.
	type Category uint32

	func (c Category) Category() string {
		return c.String()
	}

	const (
		// CatUnknown represents an unknown category. This should not be used.
		CatUnknown Category = Category(0) // Unknown
		// ADD YOUR OWN CATEGORIES HERE
	)

	//go:generate stringer -type=Type -linecomment

	// Type represents the type of the error.
	type Type uint16

	func (t Type) Type() string {
		return t.String()
	}

	const (
		// TypeUnknown represents an unknown type.
		TypeUnknown Type = Type(0) // Unknown

		// ADD YOUR OWN TYPES HERE
	)

	// LogAttrer is an interface that can be implemented by an error to return a list of attributes
	// used in logging.
	type LogAttrer = errors.LogAttrer

	// Error is the error type for this service. Error implements github.com/gostdlib/base/errors.E .
	type Error = errors.Error

	// E creates a new Error with the given parameters.
	// YOU CAN REPLACE this with your own base error constructor. See github.com/gostdlib/base/errors for more info.
	func E(ctx context.Context, c errors.Category, t errors.Type, msg error, options ...errors.EOption) Error {
	    return errors.E(ctx, c, t, msg, options...)
	}

You should include a file for your package called stdlib.go that is a copy of base/errors/stdlib/stdlib.go .
This will prevent needing to import multiple "errors" packages with renaming.

This package is meant to allow extended errors that add additional attributes to our "Error" type.
For example, you could create a SQLQueryErr like so:

	// SQLQueryErr is an example of a custom error that can be used to wrap a SQL error for more information.
	// Should be created with NewSQLQueryErr().
	type SQLQueryErr struct {
		// Query is the SQL query that was being executed.
		Query string
		// Msg is the error message from the SQL query.
		Msg   error
	}

	// NewSQLQueryErr creates a new SQLQueryErr wrapped in Error.
	func NewSQLQueryErr(q string, msg error) Error {
		return E(
			CatInternal,
			TypeUnknown,
			SQLQueryErr{
				Query: q,
				Msg:   msg,
			},
   			WithCallNum(2),
		)
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

	// Attrs implements the Attrer.Attrs() interface.
	func (s SQLQueryErr) Attrs() []slog.Attr {
		// You will notice here that I group the attributes with a category that includes the package path.
		// This is to prevent attribute name collisions with other packages.
		return []slog.Attr{slog.Group("package/path.SQLQueryErr", "Query", s.Query)}
	}

Now a program can create a compatible error that will detail our additional attributes for logging.

	// Example of creating a SQLQueryErr
	err := errors.NewSQLQueryErr("SELECT * FROM users", errors.New("SQL Error"))

In the case you want to have a more detailed top level error message than Error provides, it is simple to provide this
extra data in the error message.  Simply replace the `E` constructor in your `errors` package with custom one:

	// Args are arguments for creating our Error type.
	type Args struct {
		Category Category
		Type type
		Msg error

		ExtraField string
	}

	// Extended is an example of an the extended Error type containing the extra field.
	// This extra field will ge promoted to the top level of the log message.
	type Extended struct {
		ExtraField string
	}

	// Error returns the error message.
	func (s Extended) Error() string {
		return s.Msg.Error()
	}

	// Unwrap unwraps the error.
	func (s Extended) Unwrap() error {
		return s.Msg
	}

	// Attrs implements the Attrer.Attrs() interface.
	func (s Extended) Attrs() []slog.Attr {
		// Notice that unlike in the SQLQueryErr, we are not grouping the attributes.
		// This will cause the attributes to be at the top level of the log message.
		// This is generally only done in places like this where we are extending the base error.
		return []slog.Attr{
			slog.Any("ExtraField", s.ExtraField),
		}
	}

	// E creates a new Error with the given parameters.
	func E(ctx context.Context, args Args) E {
		return errors.E(ctx, s.Category, s.Type, Extended{ExtraField: s.ExtraField, Msg: s.Msg})
	}

Our E constructor now returns an Extended type that includes the exta field we wanted and can be used to
wrap other errors. This pattern can easily be extended to include more fields as needed if all errors require
these additional fields.

Note: This package returns concrete types.  While our constructors return our concrete type, functions or methods
returning the value should always return the error interface and never our custom "Error" concrete type.

There is a sub-directory called example/ that shows an errors package for a service which can be the
base for your package.
*/
package errors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"runtime"
	"runtime/debug"
	"time"
	"unsafe"

	ictx "github.com/gostdlib/base/internal/context"
	ierr "github.com/gostdlib/base/internal/errors"
	"github.com/gostdlib/base/telemetry/log"
	"github.com/gostdlib/base/telemetry/otel/metrics"
	"github.com/gostdlib/base/telemetry/otel/trace/span"

	"github.com/go-json-experiment/json"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	otelTrace "go.opentelemetry.io/otel/trace"
)

// LogAttrer is an interface that can be implemented by an error to return a list of attributes
// used in logging.
type LogAttrer interface {
	// LogAttrs returns a []slog.Attr that will be used in logging.
	LogAttrs(ctx context.Context) []slog.Attr
}

// TraceAttrer is an interface that can be implemented by an error to return a list of attributes
// used in tracing. Keys should be prepended by the given string. This is used by Error to
// add the package name of the error type attribute key to prevent collisions.
type TraceAttrer interface {
	TraceAttrs(ctx context.Context, prepend string, attrs span.Attributes) span.Attributes
}

// Category is the category of the error.
type Category interface {
	Category() string
}

// Type is the type of the error.
type Type interface {
	Type() string
}

type errImplements interface {
	error
	LogAttrer

	Is(target error) bool
	Unwrap() error
}

// Validate we implement the correct interfaces.
var _ errImplements = Error{}

// Error is the basic error type that is used to represent an error. Users should create their own
// "errors" package for their service with an E() method that creates a type that returns Error
// with their information. Any type that implements Error should also
// be JSON serializable for logging output.
// Error represents an error that has a category and a type. Created with E().
type Error struct {
	// Category is the category of the error. Should always be provided.
	Category Category
	// Type is the type of the error. This is a subcategory of the Category.
	// It is not always provided.
	Type Type
	// Msg is the message of the error.
	Msg error
	// MsgOveride is the message that should be used in place of the error message. This can happen
	// if the error message is sensitive or contains PII.
	MsgOverride string

	// File is the file that the error was created in. This is automatically
	// filled in by the E().
	File string
	// Line is the line that the error was created on. This is automatically
	// filled in by the E().
	Line int
	// ErrTime is the time that the error was created. This is automatically filled
	// in by E(). This is in UTC.
	ErrTime time.Time
	// StackTrace is the stack trace of the error. This is automatically filled
	// in by E() if WithStackTrace() is used.
	StackTrace string
}

// EOption is an optional argument for E().
type EOption = ierr.EOption

// WithSuppressTraceErr will prevent the trace as being recorded with an error status.
// The trace will still receive the error message. This is useful for errors that are
// retried and you only want to get a status of error if the error is not resolved.
func WithSuppressTraceErr() EOption {
	return func(e ierr.EOpts) ierr.EOpts {
		e.SuppressTraceErr = true
		return e
	}
}

// WithCallNum is used if you need to set the runtime.CallNum() in order to get the correct filename and line.
// This can happen if you create a call wrapper around E(), because you would then need to look up one more stack frame
// for every wrapper. This defaults to 1 which sets to the frame of the caller of E().
func WithCallNum(i int) EOption {
	return func(e ierr.EOpts) ierr.EOpts {
		e.CallNum = i
		return e
	}
}

// WithStackTrace will add a stack trace to the error. This is useful for debugging in certain rare
// cases. This is not recommended for general use as it can cause performance issues when errors
// are created frequently.
func WithStackTrace() EOption {
	return func(e ierr.EOpts) ierr.EOpts {
		e.StackTrace = true
		return e
	}
}

var now = time.Now

// E creates a new Error with the given parameters. If the message is already an Error, it will be returned instead.
func E(ctx context.Context, c Category, t Type, msg error, options ...EOption) Error {
	if e, ok := msg.(Error); ok {
		return e
	}

	opts := ierr.EOpts{CallNum: 1}

	// Apply local options.
	for _, o := range options {
		opts = o(opts)
	}

	// Apply call specific options.
	for _, o := range ictx.EOptions(ctx) {
		opts = o(opts)
	}

	_, filename, line, ok := runtime.Caller(opts.CallNum)
	if !ok {
		filename = "unknown"
	}

	if msg == nil {
		msg = errors.New("bug: nil error")
	}

	var st string
	if opts.StackTrace {
		st = bytesToStr(debug.Stack())
	}

	e := Error{
		Category:   c,
		Type:       t,
		File:       filename,
		Line:       line,
		Msg:        msg,
		ErrTime:    now().UTC(),
		StackTrace: st,
	}

	e.trace(ctx, opts.SuppressTraceErr)
	e.metrics()
	return e
}

// Error implements the error interface.
func (e Error) Error() string {
	if e.MsgOverride != "" {
		return e.MsgOverride
	}
	return e.Msg.Error()
}

// Is implements the errors.Is() interface. An Error is equal to another Error if the category and type are the same.
func (e Error) Is(target error) bool {
	if target == nil {
		return false
	}
	if targetE, ok := target.(Error); ok {
		return e.Category == targetE.Category && e.Type == targetE.Type
	}

	we := e.Msg
	for {
		if we == nil {
			return false
		}
		if errors.Is(we, target) {
			return true
		}
		we = errors.Unwrap(we)
	}
}

// Unwrap unwraps the error.
func (e Error) Unwrap() error {
	return e.Msg
}

// LogAttrs implements the LogAttrer.LogAttrs() interface.
func (e Error) LogAttrs(ctx context.Context) []slog.Attr {
	var (
		cat = "Unknown"
		typ = "Unknown"
	)
	if e.Category != nil {
		cat = e.Category.Category()
	}
	if e.Type != nil {
		typ = e.Type.Type()
	}

	traceID := ""
	span := span.Get(ctx)
	if span.IsRecording() {
		if span.Span.SpanContext().HasTraceID() {
			traceID = span.Span.SpanContext().TraceID().String()
		}
	}

	attrs := []slog.Attr{
		slog.String("Category", cat),
		slog.String("Type", typ),
		slog.String("ErrSrc", e.File),
		slog.Int("ErrLine", e.Line),
		slog.Time("ErrTime", e.ErrTime.UTC()),
		slog.String("TraceID", traceID),
	}
	if e.StackTrace != "" {
		attrs = append(attrs, slog.String("StackTrace", e.StackTrace))
	}

	return attrs
}

// TraceAttrs converts the error to a list of trace attributes consumable
// by the OpenTelemetry trace package. This does not include attributes on the .Msg field.
// These are added to the attrs passed in and returned.
func (e Error) TraceAttrs(ctx context.Context, prepend string, attrs span.Attributes) span.Attributes {
	if attrs.Err() != nil {
		return attrs
	}

	var (
		cat = "Unknown"
		typ = "Unknown"
	)
	if e.Category != nil {
		cat = e.Category.Category()
	}
	if e.Type != nil {
		typ = e.Type.Type()
	}

	if attrs.Attrs == nil {
		attrs.Attrs = make([]attribute.KeyValue, 0, 4)
	}

	// Unlike logging, we don't add time, as that gets recorded on the span.
	// No need for TraceID as it's already on the span.
	attrs.Add(attribute.String("Category", cat))
	attrs.Add(attribute.String("Type", typ))
	attrs.Add(attribute.String("ErrSrc", e.File))
	attrs.Add(attribute.Int("ErrLine", e.Line))

	return attrs
}

// trace adds the error to the trace span. This is automatically done when the error is created.
func (e Error) trace(ctx context.Context, suppressTraceErr bool) {
	if ctx == nil {
		return
	}

	s := span.Get(ctx)

	if !s.IsRecording() {
		return
	}

	attrs := e.TraceAttrs(ctx, "", span.Attributes{})
	for err := errors.Unwrap(e.Msg); err != nil; err = errors.Unwrap(err) {
		if t, ok := err.(TraceAttrer); ok {
			ty := reflect.TypeOf(t)
			t.TraceAttrs(ctx, ty.PkgPath()+".", attrs)
		}
	}

	options := []otelTrace.EventOption{}
	if len(attrs.Attrs) > 0 {
		options = append(options, otelTrace.WithAttributes(attrs.Attrs...))
		options = append(options, otelTrace.WithTimestamp(e.ErrTime))
	}
	s.Span.RecordError(
		e,
		options...,
	)
	if !suppressTraceErr {
		s.Status(codes.Error, e.Error())
	}
}

const meterName = "github.com/gostdlib/base/errors"

// metrics records the error in the metrics system using the Category() and Type() as the metric name
// in the format: <Category>.<Type>.
func (e Error) metrics() {
	var mp = metrics.Default()
	if mp == nil {
		return // No metrics system, nothing to do.
	}
	m := mp.Meter(meterName)
	if m == nil {
		return // No meter, nothing to do.
	}

	catStr := "unknown"
	typStr := "unknown"
	if e.Category != nil {
		catStr = e.Category.Category()
	}
	if e.Type != nil {
		typStr = e.Type.Type()
	}

	n := fmt.Sprintf("%s.%s", catStr, typStr)
	m.Int64Counter(n)
}

// Log logs the error with the given callID and customerID for easy lookup. Also
// logs the request that caused the error. The request is expected to be JSON serializable.
// If the req is a proto, this will used protojson to marshal it.
func (e Error) Log(ctx context.Context, callID, customerID string, req any) {
	var reqBytes []byte

	// Ignore serialization errors, it just means we log less information.
	switch v := req.(type) {
	case nil:
	case proto.Message:
		var err error
		reqBytes, err = protojson.Marshal(v)
		if err != nil {
			reqBytes = fmt.Appendf(reqBytes, "unable to marshal proto.Message due to error: %s", err)
		}
	case *http.Request:
		var err error
		reqBytes, err = requestToJSON(v)
		if err != nil {
			reqBytes = fmt.Appendf(reqBytes, "unable to marshal *http.Request due to error: %s", err)
		}
	default:
		log.Printf("req is %T", req)
		var err error
		reqBytes, err = json.Marshal(req)
		if err != nil {
			reqBytes = fmt.Appendf(reqBytes, "unable to marshal request %T object due to error: %s", req, err.Error())
		}
	}

	logAttrs := e.LogAttrs(ctx)
	var attrs = make([]slog.Attr, 0, 3+len(logAttrs))
	if customerID != "" {
		attrs = append(attrs, slog.Any("CustomerID", customerID))
	}
	if callID != "" {
		attrs = append(attrs, slog.Any("CallID", callID))
	}
	if len(reqBytes) > 0 {
		attrs = append(attrs, slog.Any("Request", bytesToStr(reqBytes)))
	}
	attrs = append(attrs, logAttrs...)

	// Attach any attributes from the error message.
	if f, ok := e.Msg.(LogAttrer); ok {
		attrs = append(attrs, f.LogAttrs(ctx)...)
	}

	// Look at any wrapped errors and see if they implement Attrer.
	for err := errors.Unwrap(e.Msg); err != nil; err = errors.Unwrap(err) {
		if f, ok := err.(LogAttrer); ok {
			attrs = append(attrs, f.LogAttrs(ctx)...)
		}
	}

	errMsg := "error message not provided"
	if e.Msg != nil {
		errMsg = e.Msg.Error()
	}

	log.Default().LogAttrs(ctx, slog.LevelError, errMsg, attrs...)
}

// JSONRequest is a simplified version of http.Request for JSON serialization
type JSONRequest struct {
	Method        string              `json:"method"`
	URL           string              `json:"url"`
	Header        map[string][]string `json:"header"`
	ContentLength int64               `json:"content_length"`
	Host          string              `json:"host"`
	RemoteAddr    string              `json:"remote_addr"`
	RequestURI    string              `json:"request_uri"`
	Body          string              `json:"body"`
}

func requestToJSON(r *http.Request) ([]byte, error) {
	var bodyBytes []byte
	// Read the original body
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		r.Body.Close()
	}

	// Reset the request body so it can be read again
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	jr := JSONRequest{
		Method:        r.Method,
		URL:           r.URL.String(),
		Header:        r.Header,
		ContentLength: r.ContentLength,
		Host:          r.Host,
		RemoteAddr:    r.RemoteAddr,
		RequestURI:    r.RequestURI,
		Body:          bytesToStr(bodyBytes),
	}
	return json.Marshal(jr)
}

// bytesToStr converts a byte slice to a string without copying the data.
func bytesToStr(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(&b[0], len(b))
}
