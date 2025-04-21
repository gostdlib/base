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
	"reflect"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/go-json-experiment/json"
	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/telemetry/otel/trace/span"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// State is a function that takes a Request and returns a Request. If the returned Request has a nil Next, the state machine stops.
// If the returned Request has a non-nil Err, the state machine stops and returns the error. If the returned Request has a non-nil
// next, the state machine continues with the next state.
type State[T any] func(req Request[T]) Request[T]

// seenStagesPool is a pool of seenStages objects to reduce allocations.
var seenStagesPool = sync.NewPool(
	context.Background(),
	"",
	func() *seenStages {
		return &seenStages{}
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
	for _, st := range *s {
		if st == stage {
			return true
		}
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

// reset resets the seenStages object to be reused.
func (s *seenStages) reset() *seenStages {
	n := (*s)[:0]
	s = &n
	return s
}

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

	// Defer is a function that is called when the state machine stops. This function can change the data
	// passed an it will modify Request.Data before it is returned by Run(). err indicates if you had an
	// error and what it was, otherwise the Request completed.
	Defer func(ctx context.Context, data T, err error) T

	// seenStages tracks what stages have been called in this Request. This is used to
	// detect cyclic errors. If nil, cyclic errors are not checked.
	seenStages *seenStages
}

func (r Request[T]) otelStart() Request[T] {
	if r.span.Span == nil || !r.span.Span.IsRecording() {
		return r
	}

	j, err := json.Marshal(r.Data)
	if err != nil {
		j = []byte(fmt.Sprintf("Error marshaling data: %s", err.Error()))
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
		j = []byte(fmt.Sprintf("Error marshaling data: %s", err.Error()))
	}
	end := time.Now()
	r.Event(
		"statemachine processing end",
		attribute.String("data", bytesToStr(j)),
		attribute.Int64("elapsed_ns", end.Sub(r.startTime).Nanoseconds()),
	)
	r.span.End()
}

// Option is an option for the Run() function.
// This is currently unused, but exists for future expansion.
type Option[T any] func(Request[T]) (Request[T], error)

var (
	nameEmptyErr = fmt.Errorf("name is empty")
	ctxNilErr    = fmt.Errorf("Request.Ctx is nil")
	nextNilErr   = fmt.Errorf("Request.Next is nil, must be set to the initial state")
	reqErrNotNil = fmt.Errorf("Request.Err is not nil")
)

// Run runs the state machine with the given a Request. name is the name of the statemachine for the
// purpose of OTEL tracing. An error is returned if the state machine fails, name
// is empty, the Request Ctx/Next is nil or the Err field is not nil.
func Run[T any](name string, req Request[T], options ...Option[T]) (Request[T], error) {
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

	for _, o := range options {
		var err error
		req, err = o(req)
		if err != nil {
			return req, err
		}
	}

	if req.span.Span != nil && req.span.Span.IsRecording() {
		req.Ctx, req.span = span.New(req.Ctx, span.WithName(fmt.Sprintf("statemachine(%s)", name)))
		req.otelStart()
		defer req.otelEnd()
	}

	for req.Next != nil {
		var stateName string
		stateName, req = execState(req)
		if req.Err != nil {
			req = execDefer(req)
			req.span.Status(codes.Error, fmt.Sprintf("error in State(%s): %s", stateName, req.Err.Error()))
			return req, req.Err
		}
	}

	return execDefer(req), nil
}

// execDefer executes the Request.Defer function if it is not nil.
func execDefer[T any](req Request[T]) Request[T] {
	if req.Defer == nil {
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
	req.Data = req.Defer(req.Ctx, req.Data, req.Err)
	return req
}

var execReqNextNil = fmt.Errorf("bug: execState received Request.Next == nil")

// execState executes Request.Next state and returns the Request.
func execState[T any](req Request[T]) (string, Request[T]) {
	if req.Next == nil {
		req.Err = execReqNextNil
		return "", req
	}

	state := req.Next
	stateName := methodName(state)

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
	return stateName, state(req)
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
