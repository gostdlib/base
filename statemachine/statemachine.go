/*
Package statemachine provides a simple routing state machine implementation. This is useful for implementing
complex state machines that require routing logic. The state machine is implemented as a series of state functions
that take a Request and returns a Request with the next State or an error.
An error causes the state machine to stop and return the error. A nil state causes the state machine to stop.

You may build a state machine using either function calls or method calls. The Request.Data object you define
can be a stack or heap allocated object. Using a stack allocated object is useful when running a lot of state machines
in parallel, as it reduces the amount of memory allocation and garbage collection required.

State machines of this design can reduce testing complexity and improve code readability. You can read about how here:
https://medium.com/@johnsiilver/go-state-machine-patterns-3b667f345b5e

This package is has OTEL support built in. If the Context passed to the state machine has a span, the state machine
will create a child span for each state. If the state machine returns an error, the span will be marked as an error.

There are advanced logging per request options, cyclic state detection, and pre/post state wrapping options to allow
common logic to be executed before or after each state.

Example:

		package main

		import (
			"context"
			"fmt"
			"io"
			"log"
			"net/http"

			"github.com/gostdlib/ops/statemachine"
		)

		var (
			author = flag.String("author", "", "The author of the quote, if not set will choose a random one")
		)

		// Data is the data passed to through the state machine. It can be modified by the state functions.
		type Data struct {
			// This section is data set before starting the state machine.

			// Author is the author of the quote. If not set it will be chosen at random.
			Author string

			// This section is data set during the state machine.

			// Quote is a quote from the author. It is set in the state machine.
			Quote string

			// httpClient is the http client used to make requests.
			httpClient *http.Client
		}

		func Start(req statemachine.Request[Data]) statemachine.Request[Data] {
			if req.Data.httpClient == nil {
				req.Data.httpClient = &http.Client{}
			}

			if req.Data.Author == "" {
				req.Next = RandomAuthor
				return req
			}
			req.Next = RandomQuote
			return req
		}

		func RandomAuthor(req statemachine.Request[Data]) statemachine.Request[Data] {
			const url = "https://api.quotable.io/randomAuthor" // This is a fake URL
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				req.Err = err
				return req
			}

			req = req.WithContext(ctx)
			resp, err := args.Data.httpClient.Do(req)
			if err != nil {
				req.Err = err
				return req
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				req.Err = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
				return req
			}
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				req.Err = err
				return req
			}
			args.Data.Author = string(b)
			req.Next = RandomQuote
			return req
		}

		func RandomQuote(req statemachine.Request[Data]) statemachine.Request[Data] {
			const url = "https://api.quotable.io/randomQuote" // This is a fake URL
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				req.Err = err
				return req
			}

			req = req.WithContext(ctx)
			resp, err := args.Data.httpClient.Do(req)
			if err != nil {
				req.Err = err
				return req
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				req.Err = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
				return req
			}
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				req.Err = err
				return req
			}
			args.Data.Quote = string(b)
			req.Next = nil  // This is not needed, but a good way to show that the state machine is done.
			return req
		}

		func main() {
			flag.Parse()

			req := statemachine.Request{
	  			Ctx: context.Background(),
	     			Data: Data{
					Author: *author,
					httpClient: &http.Client{},
				},
	   			Next: Start,
			}

			err := statemachine.Run("Get author quotes", req)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(data.Author, "said", data.Quote)
		}
*/
package statemachine

import (
	"fmt"
	"log/slog"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/go-json-experiment/json"
	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/telemetry/otel/trace/span"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// ErrValidation is an error when trying to run a state machine with invalid arguments.
type ErrValidation struct {
	msg string
}

// Error implements the error interface.
func (v ErrValidation) Error() string {
	return v.msg
}

var (
	nameEmptyErr = ErrValidation{"name is empty"}
	ctxNilErr    = ErrValidation{"Request.Ctx is nil"}
	nextNilErr   = ErrValidation{"Request.Next is nil, must be set to the initial state"}
	reqErrNotNil = ErrValidation{"Request.Err is not nil"}
)

// ErrCyclic is an error that is returned when a state machine detects a cyclic error.
type ErrCyclic struct {
	// SMName is the name of the state machine that detected the cyclic error.
	SMName string
	// LastStage is the name of the stage that would have been executed a second time. This caused the cyclic error.
	LastStage string
	// Stages lists the stages that were executed before the cyclic error was detected.
	Stages string
}

func newCyclicError(smName, lastStage string, stages *seenStages) error {
	return ErrCyclic{
		SMName:    smName,
		LastStage: lastStage,
		Stages:    stages.callTrace(),
	}
}

// Error implements the error interface.
func (c ErrCyclic) Error() string {
	return fmt.Sprintf("statemachine(%s) had cyclic error: stage(%s): callTrace: %s\n", c.SMName, c.LastStage, c.Stages)
}

// Is implements the errors.Is interface.
func (c ErrCyclic) Is(err error) bool {
	if _, ok := err.(ErrCyclic); ok {
		return true
	}
	return false
}

// Attrs implements the base/errors.Attrs interface.
func (c ErrCyclic) Attrs() []slog.Attr {
	return []slog.Attr{
		slog.String("statemachine", c.SMName),
		slog.String("last_stage", c.LastStage),
		slog.String("call_trace", c.Stages),
	}
}

// State is a function that takes a Request and returns a Request. If the returned Request has a nil Next, the state machine stops.
// If the returned Request has a non-nil Err, the state machine stops and returns the error. If the returned Request has a non-nil
// next, the state machine continues with the next state.
type State[T any] func(req Request[T]) Request[T]

// String implements fmt.Stringer interface, returning the name of the state function.
func (s State[T]) String() string {
	return MethodName(s)
}

// seenStagesPool is a pool of seenStages objects to reduce allocations.
var seenStagesPool = sync.NewPool(
	context.Background(),
	"seenStagesPool",
	func() *seenStages {
		ss := make(seenStages, 0, 1)
		return &ss
	},
)

// seenStages tracks what stages have been called in a Request. This is used to detect
// cyclic errors. Implemented with a slice to reduce allocations and is faster to
// remove elements from the slice than a map (to allow reuse). n is small, so the
// lookup performance is negligible. This is not thread-safe (which is not needed).
type seenStages []string

// seen returns true if the stage has been seen before. If it has not been seen,
// it adds it to the list of seen stages.
func (s *seenStages) seen(stage string) bool {
	if slices.Contains(*s, stage) {
		return true
	}

	n := append(*s, stage)
	*s = n
	return false
}

// callTrace returns a string of the stages that have been called.
func (s *seenStages) callTrace() string {
	out := strings.Builder{}
	for i, st := range *s {
		if i != 0 {
			out.WriteString(" -> ")
		}
		out.WriteString(st)
	}
	return out.String()
}

// Reset resets the seenStages object to be reused. Implements concurrency/sync.Resetter.
func (s *seenStages) Reset() *seenStages {
	n := (*s)[:0]
	s = &n
	return s
}

// Defer is a function that is called when the state machine stops. This function can change the data
// passed and it will modify Request.Data before it is returned by Run(). err indicates if you had an
// error and what it was, otherwise the Request completed.
type DeferFn[T any] func(ctx context.Context, data T, err error) T

var logCounters = atomic.Uint32{}

// Request are the request passed to a state function.
type Request[T any] struct {
	span span.Span

	startTime time.Time

	// Ctx is the context passed to the state function.
	Ctx context.Context

	// Data is the data to be passed to the next state.
	Data T

	// Err is the error to be returned by the state machine. If Err is not nil, the state machine stops.
	Err error

	// Next is the next state to be executed. If Next is nil, the state machine stops.
	// Must be set to the initial state to execute before calling Run().
	Next State[T]

	// Defers is a list of functions to be called when the state machine stops. This is
	// useful for cleaning up resources or modifying the data before it is returned.
	Defers []DeferFn[T]

	// seenStages tracks what stages have been called in this Request. This is used to
	// detect cyclic errors. If nil, cyclic errors are not checked.
	seenStages *seenStages
	// logStages indicates if each stage should be logged as it is executed. If true, each stage will be logged.
	logStages bool
	// reqID is a unique identifier for this Request. This is used for logging purposes.
	reqID string
	// preWrap are functions that are called before each state is executed.
	// postWrap are functions that are called after each state is executed.
	preWrap, postWrap []Wrap[T]
}

func (r Request[T]) otelStart() Request[T] {
	if r.span.Span == nil || !r.span.Span.IsRecording() {
		return r
	}

	j, err := json.Marshal(r.Data)
	if err != nil {
		j = fmt.Appendf([]byte{}, "Error marshaling data: %s", err.Error())
	}

	r.startTime = time.Now()
	r.span.Event(
		"statemachine processing start",
		attribute.String("data", bytesToStr(j)),
	)
	return r
}

func bytesToStr(b []byte) string {
	return unsafe.String(&b[0], len(b))
}

/*
Event records an OTEL event into the Request span with name and keyvalues. This allows for stages
in your statemachine to record information in each state.

Note: This is a no-op if the Request is not recording.
*/
func (r Request[T]) Event(name string, keyValues ...attribute.KeyValue) {
	if r.span.Span == nil || !r.span.Span.IsRecording() {
		return // No-op
	}

	r.span.Event(name, keyValues...)
}

func (r Request[T]) otelEnd() {
	if r.span.Span == nil || !r.span.Span.IsRecording() {
		return
	}
	if r.Err != nil {
		r.span.Status(codes.Error, r.Err.Error())
		return
	}
	j, err := json.Marshal(r.Data)
	if err != nil {
		j = fmt.Appendf([]byte{}, "Error marshaling data: %s", err.Error())
	}
	end := time.Now()
	r.Event(
		"statemachine processing end",
		attribute.String("data", bytesToStr(j)),
		attribute.Int64("elapsed_ns", end.Sub(r.startTime).Nanoseconds()),
	)
	r.span.End()
}

type allOptions struct {
	requestModifiers []any // []RequestOption
	preState         []any // []func(req Request[T], state State[T]) error
	postState        []any // []func(req Request[T], state State[T]) error
}

// Option is an option for the Run() function.
type Option func(o allOptions) (allOptions, error)

// PrePostWrap is a function that wraps a state function. It is called before or after the state function is executed. Changing
// the Request.Next will not modify the state machine flow. Modifying the Request.Err has undefined behavior. If you wish to
// stop the state machine, return an error. This allows you to add common logic before or after each state is executed.
// Used by WithPreWrap and WithPostWrap.
type Wrap[T any] func(req Request[T], state State[T]) (Request[T], error)

// WithPreWrap adds functions that are called before each state is executed. If the function
// returns an error, the state machine stops and returns the error. Use this to simply the
// state machine by handling common logic in one place. You can use this to add multiple pre-state
func WithPreWrap[T any](w ...Wrap[T]) Option {
	return func(o allOptions) (allOptions, error) {
		for _, f := range w {
			if f == nil {
				return o, fmt.Errorf("WithPreState: function cannot be nil")
			}
			o.preState = append(o.preState, f)
		}
		return o, nil
	}
}

// WithPostWrap adds functions that are called after each state is executed. If the function
// returns an error, the state machine stops and returns the error. Use this to simply the
// state machine by handling common logic in one place.
func WithPostWrap[T any](w ...Wrap[T]) Option {
	return func(o allOptions) (allOptions, error) {
		for _, f := range w {
			if f == nil {
				return o, fmt.Errorf("WithPostState: function cannot be nil")
			}
			o.postState = append(o.postState, f)
		}
		return o, nil
	}
}

// RequestOption is an option that modifies the Request, usually for debug purposes.
type RequestOption[T any] func(req Request[T]) (Request[T], error)

// WithRequestOption adds a RequestOption to the Run() function options. Use this to pass CyclicCheck or LogStages.
// We do not support custom options, so do that at your own risk.
func WithRequestOptions[T any](ros ...RequestOption[T]) Option {
	return func(o allOptions) (allOptions, error) {
		for _, ro := range ros {
			o.requestModifiers = append(o.requestModifiers, ro)
		}
		return o, nil
	}
}

// CyclicCheck is a RequestOption that causes the state machine to error if a state is called more than once.
// This effectively turns the state machine into a directed acyclic graph.
func CyclicCheck[T any](req Request[T]) (Request[T], error) {
	req.seenStages = seenStagesPool.Get(req.Ctx)
	return req, nil
}

// LogStages is a RequestOption that causes the state machine to log each stage as it is executed at Info level. Useful for debugging.
// Note that with an active OTEL span, more information will be available, but if you do not have OTEL enabled, this will
// provide some insight into what stages are being executed and where errors or timeouts are. If this has a
// ID() string method on .Data, the ID will be recorded.
func LogStages[T any](req Request[T]) (Request[T], error) {
	type ider interface {
		ID() string
	}

	req.logStages = true
	if idObj, ok := any(req.Data).(ider); ok {
		req.reqID = idObj.ID()
	}
	return req, nil
}

// Run runs the state machine with the given a Request. name is the name of the statemachine for the
// purpose of OTEL tracing. An error is returned if the state machine fails, name
// is empty, the Request Ctx/Next is nil or the Err field is not nil.
func Run[T any](name string, req Request[T], options ...Option) (Request[T], error) {
	defer func() {
		if req.seenStages != nil {
			seenStagesPool.Put(req.Ctx, req.seenStages)
		}
	}()

	if strings.TrimSpace(name) == "" {
		req.Next = nil
		return req, nameEmptyErr
	}
	if req.Ctx == nil {
		req.Next = nil
		return req, ctxNilErr
	}
	if req.Next == nil {
		req.Next = nil
		return req, nextNilErr
	}
	if req.Err != nil {
		req.Next = nil
		return req, reqErrNotNil
	}

	opts := allOptions{}
	for _, o := range options {
		var err error
		opts, err = o(opts)
		if err != nil {
			req.Next = nil
			return req, err
		}
	}

	for _, o := range opts.requestModifiers {
		var err error
		if o == nil {
			continue
		}
		ro := o.(RequestOption[T])
		req, err = ro(req)
		if err != nil {
			return req, err
		}
	}

	req.preWrap = make([]Wrap[T], 0, len(opts.preState))
	req.postWrap = make([]Wrap[T], 0, len(opts.postState))
	for _, o := range opts.preState {
		req.preWrap = append(req.preWrap, o.(Wrap[T]))
	}
	for _, o := range opts.postState {
		req.postWrap = append(req.postWrap, o.(Wrap[T]))
	}

	if req.span.Span != nil && req.span.Span.IsRecording() {
		req.Ctx, req.span = span.New(req.Ctx, span.WithName(fmt.Sprintf("statemachine(%s)", name)))
		req.otelStart()
		defer req.otelEnd()
	}

	for req.Next != nil {
		stateName := methodName(req.Next)
		if req.seenStages != nil {
			if req.seenStages.seen(stateName) {
				req.Next = nil
				return req, newCyclicError(name, stateName, req.seenStages)
			}
		}
		if req.logStages {
			context.Log(req.Ctx).Info("statemachine executing state: "+stateName, slog.String("statemachine", name), slog.String("state", stateName), slog.String("reqID", req.reqID))
		}
		req = execState(req, stateName)

		if req.logStages {
			context.Log(req.Ctx).Info("statemachine finished state: "+stateName, slog.String("statemachine", name), slog.String("state", stateName), slog.String("reqID", req.reqID))
		}
		if req.Err != nil {
			req = execDefer(req)
			if req.logStages {
				context.Log(req.Ctx).Info(fmt.Sprintf("statemachine state(%s) error: %s", stateName, req.Err), slog.String("statemachine", name), slog.String("state", stateName), slog.String("reqID", req.reqID))
			}
			req.span.Status(codes.Error, fmt.Sprintf("error in State(%s): %s", stateName, req.Err.Error()))
			return req, req.Err
		}
	}

	return execDefer(req), nil
}

// execDefer executes the Request.Defer function if it is not nil.
func execDefer[T any](req Request[T]) Request[T] {
	if req.Defers == nil {
		return req
	}
	if req.span.Span != nil && req.span.Span.IsRecording() {
		parentCtx := req.Ctx
		parentSpan := req.span
		defer func() {
			req.Ctx = parentCtx
			req.span = parentSpan
		}()

		req.Ctx, req.span = span.New(req.Ctx, span.WithName("Defer call"))
	}
	for i := len(req.Defers) - 1; i >= 0; i-- {
		req.Data = req.Defers[i](req.Ctx, req.Data, req.Err)
	}
	return req
}

var execReqNextNil = fmt.Errorf("bug: execState received Request.Next == nil")

// execState executes Request.Next state and returns the Request.
func execState[T any](req Request[T], stateName string) Request[T] {
	if req.Next == nil {
		req.Err = execReqNextNil
		return req
	}

	state := req.Next

	if req.span.Span != nil && req.span.Span.IsRecording() {
		parentCtx := req.Ctx
		parentSpan := req.span
		defer func() {
			req.Ctx = parentCtx
			req.span = parentSpan
		}()

		req.Ctx, req.span = span.New(req.Ctx, span.WithName(fmt.Sprintf("State(%s)", stateName)))
	}

	req.Next = nil

	var err error
	for _, pre := range req.preWrap {
		req, err = pre(req, state)
		if err != nil {
			req.Next = nil
			req.Err = err
			return req
		}
	}
	req = state(req)
	for _, post := range req.postWrap {
		req, err = post(req, state)
		if err != nil {
			req.Next = nil
			if req.Err == nil {
				req.Err = err
			} else {
				req.Err = fmt.Errorf("multiple errors in postWrap: %v; %v", req.Err, err)
			}
		}
	}
	return req
}

// methodName takes a function or a method and returns its name.
func methodName(method any) string {
	if method == nil {
		return "<nil>"
	}
	valueOf := reflect.ValueOf(method)
	switch valueOf.Kind() {
	case reflect.Func:
		return strings.TrimSuffix(strings.TrimSuffix(runtime.FuncForPC(valueOf.Pointer()).Name(), "-fm"), "[...]")
	default:
		return "<not a function>"
	}
}

// MethodName takes a function or a method and returns its name. This is useful in testing
// to determine the name of the next state in a state machine and do a comparison.
func MethodName(method any) string {
	if method == nil {
		return "<nil>"
	}
	valueOf := reflect.ValueOf(method)
	switch valueOf.Kind() {
	case reflect.Func:
		return strings.TrimSuffix(strings.TrimSuffix(runtime.FuncForPC(valueOf.Pointer()).Name(), "-fm"), "[...]")
	default:
		return "<not a function>"
	}
}
