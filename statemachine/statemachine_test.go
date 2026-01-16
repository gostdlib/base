package statemachine

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/gostdlib/base/errors"
	"github.com/kylelemons/godebug/pretty"
)

type data struct {
	Num int
}

func (d data) ID() string {
	return fmt.Sprintf("data-%d", d.Num)
}

func steer(req Request[data]) Request[data] {
	switch req.Data.Num {
	case 0:
		req.Next = nil
	case math.MaxInt:
		req.Next = addErr
	case 30:
		req.Next = addDefer
	default:
		req.Next = addTen
	}
	return req
}

func addDefer(req Request[data]) Request[data] {
	req.Defers = append(req.Defers, func(ctx context.Context, d data, err error) data {
		d.Num += 90
		return d
	})
	req.Next = addDefer2
	return req
}

func addDefer2(req Request[data]) Request[data] {
	req.Defers = append(req.Defers, func(ctx context.Context, d data, err error) data {
		d.Num = d.Num * 2
		return d
	})
	req.Next = nil
	return req
}

func addTen(req Request[data]) Request[data] {
	req.Data.Num += 10
	req.Next = nil
	return req
}

func addErr(req Request[data]) Request[data] {
	req.Err = fmt.Errorf("addErr")
	return req
}

const cyclicStages = `github.com/gostdlib/base/statemachine.(*cyclicCheck).start -> github.com/gostdlib/base/statemachine.(*cyclicCheck).stage1 -> github.com/gostdlib/base/statemachine.(*cyclicCheck).stage2`

type cyclicCheck struct {
	stage2Done bool
}

func (c *cyclicCheck) start(req Request[data]) Request[data] {
	req.Next = c.stage1
	return req
}

func (c *cyclicCheck) stage1(req Request[data]) Request[data] {
	if c.stage2Done {
		panic("stage1 called after stage2")
	}
	req.Next = c.stage2
	return req
}

func (c *cyclicCheck) stage2(req Request[data]) Request[data] {
	req.Next = c.stage1
	c.stage2Done = true
	return req
}

func TestStateString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state State[data]
		want  string
	}{
		{
			name:  "Success: steer function returns correct name",
			state: steer,
			want:  "github.com/gostdlib/base/statemachine.steer",
		},
		{
			name:  "Success: addTen function returns correct name",
			state: addTen,
			want:  "github.com/gostdlib/base/statemachine.addTen",
		},
		{
			name:  "Success: addErr function returns correct name",
			state: addErr,
			want:  "github.com/gostdlib/base/statemachine.addErr",
		},
		{
			name:  "Success: addDefer function returns correct name",
			state: addDefer,
			want:  "github.com/gostdlib/base/statemachine.addDefer",
		},
		{
			name:  "Success: addDefer2 function returns correct name",
			state: addDefer2,
			want:  "github.com/gostdlib/base/statemachine.addDefer2",
		},
	}

	for _, test := range tests {
		got := test.state.String()
		if got != test.want {
			t.Errorf("TestStateString(%s): got %q, want %q", test.name, got, test.want)
		}
	}
}

func TestPreWrapAndPostWrap(t *testing.T) {
	t.Parallel()

	parentCtx := context.Background()

	preWrapAddFive := func(req Request[data], state State[data]) (Request[data], error) {
		req.Data.Num += 5
		return req, nil
	}

	preWrapAddTwo := func(req Request[data], state State[data]) (Request[data], error) {
		req.Data.Num += 2
		return req, nil
	}

	preWrapError := func(req Request[data], state State[data]) (Request[data], error) {
		return req, fmt.Errorf("preWrap error")
	}

	postWrapMultiplyTwo := func(req Request[data], state State[data]) (Request[data], error) {
		req.Data.Num = req.Data.Num * 2
		return req, nil
	}

	postWrapAddThree := func(req Request[data], state State[data]) (Request[data], error) {
		req.Data.Num += 3
		return req, nil
	}

	postWrapError := func(req Request[data], state State[data]) (Request[data], error) {
		return req, fmt.Errorf("postWrap error")
	}

	tests := []struct {
		name     string
		req      Request[data]
		options  []Option
		wantData data
		wantErr  bool
	}{
		{
			name: "Success: preWrap modifies data before state execution",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: addTen,
				Data: data{Num: 0},
			},
			options:  []Option{WithPreWrap(preWrapAddFive)},
			wantData: data{Num: 15},
		},
		{
			name: "Success: postWrap modifies data after state execution",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: addTen,
				Data: data{Num: 0},
			},
			options:  []Option{WithPostWrap(postWrapMultiplyTwo)},
			wantData: data{Num: 20},
		},
		{
			name: "Success: multiple preWrap functions execute in order",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: addTen,
				Data: data{Num: 0},
			},
			options:  []Option{WithPreWrap(preWrapAddFive, preWrapAddTwo)},
			wantData: data{Num: 17},
		},
		{
			name: "Success: multiple postWrap functions execute in order",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: addTen,
				Data: data{Num: 10},
			},
			options:  []Option{WithPostWrap(postWrapMultiplyTwo, postWrapAddThree)},
			wantData: data{Num: 43},
		},
		{
			name: "Success: both preWrap and postWrap modify data",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: addTen,
				Data: data{Num: 0},
			},
			options:  []Option{WithPreWrap(preWrapAddFive), WithPostWrap(postWrapMultiplyTwo)},
			wantData: data{Num: 30},
		},
		{
			name: "Error: preWrap returns error stops execution",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: addTen,
				Data: data{Num: 0},
			},
			options:  []Option{WithPreWrap(preWrapError)},
			wantData: data{Num: 0},
			wantErr:  true,
		},
		{
			name: "Error: postWrap returns error stops execution",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: addTen,
				Data: data{Num: 0},
			},
			options:  []Option{WithPostWrap(postWrapError)},
			wantData: data{Num: 10},
			wantErr:  true,
		},
		{
			name: "Success: preWrap and postWrap work with multiple state transitions",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: steer,
				Data: data{Num: 5},
			},
			options:  []Option{WithPreWrap(preWrapAddTwo), WithPostWrap(postWrapAddThree)},
			wantData: data{Num: 25},
		},
	}

	for _, test := range tests {
		gotReq, err := Run("test", test.req, test.options...)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestPreWrapAndPostWrap(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestPreWrapAndPostWrap(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
		if diff := pretty.Compare(test.wantData, gotReq.Data); diff != "" {
			t.Errorf("TestPreWrapAndPostWrap(%s): Data: -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	cc := cyclicCheck{}

	tests := []struct {
		name      string
		argName   string
		req       Request[data]
		options   []Option
		wantReq   Request[data]
		wantErr   bool
		cyclicErr bool
	}{
		{
			name: "Error: name is not set",
			req: Request[data]{
				Ctx:  context.Background(),
				Next: steer,
				Data: data{Num: 0},
			},
			wantReq: Request[data]{Ctx: context.Background()},
			wantErr: true,
		},
		{
			name:    "Error: ctx is nil",
			argName: "test",
			req: Request[data]{
				Next: steer,
				Data: data{Num: 0},
			},
			wantReq: Request[data]{Ctx: nil},
			wantErr: true,
		},
		{
			name:    "Error: Next is nil",
			argName: "test",
			req: Request[data]{
				Ctx:  context.Background(),
				Data: data{Num: 0},
			},
			wantReq: Request[data]{Ctx: context.Background()},
			wantErr: true,
		},
		{
			name:    "Error: Err is not nil",
			argName: "test",
			req: Request[data]{
				Ctx:  context.Background(),
				Next: steer,
				Err:  fmt.Errorf("testErr"),
			},
			wantReq: Request[data]{Ctx: context.Background(), Err: fmt.Errorf("testErr")},
			wantErr: true,
		},
		{
			name:    "Success",
			argName: "test",
			req: Request[data]{
				Ctx:  context.Background(),
				Next: steer,
				Data: data{Num: 1},
			},
			wantReq: Request[data]{Ctx: context.Background(), Data: data{Num: 11}},
		},
		{
			name:    "Success with defers",
			argName: "test",
			req: Request[data]{
				Ctx:  context.Background(),
				Next: steer,
				Data: data{Num: 30},
			},
			// This is not 240, because the first defer is executed after the second.
			wantReq: Request[data]{Ctx: context.Background(), Data: data{Num: 150}},
		},
		{
			name:    "Fail: cyclic check",
			argName: "test",
			req: Request[data]{
				Ctx:  context.Background(),
				Next: cc.start,
				Data: data{Num: 30},
			},
			options:   []Option{WithRequestOptions(CyclicCheck[data], LogStages[data])},
			wantErr:   true,
			cyclicErr: true,
		},
	}

	for _, test := range tests {
		gotReq, err := Run(test.argName, test.req, test.options...)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestRun(%s): got err == nil, want err != nil", test.name)
		case err != nil && !test.wantErr:
			t.Errorf("TestRun(%s): got err == %s, want err == nil", test.name, err)
		case err != nil:
			if test.cyclicErr {
				if !errors.Is(err, ErrCyclic{}) {
					t.Errorf("TestRun(%s): got err == %T, want err == %T", test.name, err, ErrCyclic{})
				}

				cErr := ErrCyclic{}
				if !errors.As(err, &cErr) {
					t.Errorf("TestRun(%s): errors.As(): got err == %T, want err == %T", test.name, err, ErrCyclic{})
				}
				if cErr.Stages != cyclicStages {
					t.Errorf("TestRun(%s): got stages == %s, want stages == %s", test.name, cErr.Stages, cyclicStages)
				}
			}
			continue
		}
		gotReq.Defers = nil // Reset defers to nil after execution to avoid comparison.
		// No one cares about context values.
		test.wantReq.Ctx = context.Background()
		gotReq.Ctx = context.Background()
		if diff := pretty.Compare(test.wantReq, gotReq); diff != "" {
			t.Errorf("TestRun(%s): got diff (-want +got):\n%s", test.name, diff)
		}
	}
}

func TestExecState(t *testing.T) {
	t.Parallel()

	parentCtx := context.Background()

	tests := []struct {
		name        string
		req         Request[data]
		wantRequest Request[data]
	}{
		{
			name: "Error: Request.Next == nil",
			req: Request[data]{
				Ctx: parentCtx,
			},
			wantRequest: Request[data]{Ctx: parentCtx, Err: fmt.Errorf("bug: execState received Request.Next == nil")},
		},
		{
			name: "Route to addTen",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: steer,
				Data: data{Num: 1},
			},
			wantRequest: Request[data]{Ctx: parentCtx, Data: data{Num: 1}, Next: addTen},
		},
		{
			name: "Route to addErr",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: steer,
				Data: data{Num: math.MaxInt},
			},
			wantRequest: Request[data]{Ctx: parentCtx, Data: data{Num: math.MaxInt}, Next: addErr},
		},
		{
			name: "Route to nil",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: steer,
				Data: data{Num: 0},
			},
			wantRequest: Request[data]{Ctx: parentCtx, Data: data{Num: 0}, Next: nil},
		},
		{
			name: "Check data change in addTen",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: addTen,
				Data: data{Num: 1},
			},
			wantRequest: Request[data]{Ctx: parentCtx, Data: data{Num: 11}, Next: nil},
		},
		{
			name: "Check error in addErr",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: addErr,
				Data: data{Num: 1},
			},
			wantRequest: Request[data]{Ctx: parentCtx, Data: data{Num: 1}, Err: fmt.Errorf("addErr")},
		},
	}

	for _, test := range tests {
		gotRequest := execState(test.req, "name")
		if diff := pretty.Compare(test.wantRequest, gotRequest); diff != "" {
			t.Errorf("TestExecState(%s): Request: -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestExecDefer(t *testing.T) {
	t.Parallel()

	parentCtx := context.Background()

	deferFn := func(ctx context.Context, d data, err error) data {
		if err != nil {
			d.Num = -5
			return d
		}
		d.Num += 10
		return d
	}

	tests := []struct {
		name   string
		req    Request[data]
		defers []DeferFn[data]
		want   Request[data]
	}{
		{
			name: "No Defer function",
			req: Request[data]{
				Ctx:  parentCtx,
				Data: data{Num: 5},
			},
			want: Request[data]{
				Ctx:  parentCtx,
				Data: data{Num: 5},
			},
		},
		{
			name: "Defer function modifies data without error",
			req: Request[data]{
				Ctx:  parentCtx,
				Data: data{Num: 5},
			},
			defers: []DeferFn[data]{deferFn},
			want: Request[data]{
				Ctx:  parentCtx,
				Data: data{Num: 15},
			},
		},
		{
			name: "Defer function modifies data with error",
			req: Request[data]{
				Ctx:  parentCtx,
				Data: data{Num: 5},
				Err:  fmt.Errorf("initial error"),
			},
			defers: []DeferFn[data]{deferFn},
			want: Request[data]{
				Ctx:  parentCtx,
				Data: data{Num: -5},
				Err:  fmt.Errorf("initial error"),
			},
		},
	}

	for _, test := range tests {
		test.req.Defers = test.defers
		got := execDefer(test.req)
		got.Defers = nil // Reset defers to nil after execution to avoid comparison.
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestExecDefer(%s): Request: -want/+got:\n%s", test.name, diff)
		}
	}
}

func functionA() {
	fmt.Println("Function A")
}

func functionB() {
	fmt.Println("Function B")
}

func genericFunc[T any](v T) {
	fmt.Println(v)
}

func TestMethodName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   interface{}
		want string
	}{
		{"functionA", functionA, "github.com/gostdlib/base/statemachine.functionA"},
		{"functionB", functionB, "github.com/gostdlib/base/statemachine.functionB"},
		{"genericFunc", genericFunc[string], "github.com/gostdlib/base/statemachine.genericFunc"},
	}

	for _, test := range tests {
		got := methodName(test.fn)
		if got != test.want {
			t.Errorf("TestMethodName(%s): got %q, want %q", test.name, got, test.want)
		}
	}
}
