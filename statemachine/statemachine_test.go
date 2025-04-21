package statemachine

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

type data struct {
	Num int
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

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		argName string
		req     Request[data]
		wantReq Request[data]
		wantErr bool
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
	}

	for _, test := range tests {
		gotReq, err := Run(test.argName, test.req)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestRun(%s) got err == nil, want err != nil", test.name)
		case err != nil && !test.wantErr:
			t.Errorf("TestRun(%s) got err == %s, want err == nil", test.name, err)
		}
		gotReq.Defers = nil // Reset defers to nil after execution to avoid comparison.
		if diff := pretty.Compare(test.wantReq, gotReq); diff != "" {
			t.Errorf("TestRun(%s) got diff (-want +got):\n%s", test.name, diff)
		}
	}
}

func TestExecState(t *testing.T) {
	t.Parallel()

	parentCtx := context.Background()

	tests := []struct {
		name          string
		req           Request[data]
		wantStateName string
		wantRequest   Request[data]
	}{
		{
			name: "Error: Request.Next == nil",
			req: Request[data]{
				Ctx: parentCtx,
			},
			wantStateName: "",
			wantRequest:   Request[data]{Ctx: parentCtx, Err: fmt.Errorf("bug: execState received Request.Next == nil")},
		},
		{
			name: "Route to addTen",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: steer,
				Data: data{Num: 1},
			},
			wantStateName: "github.com/gostdlib/base/statemachine.steer",
			wantRequest:   Request[data]{Ctx: parentCtx, Data: data{Num: 1}, Next: addTen},
		},
		{
			name: "Route to addErr",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: steer,
				Data: data{Num: math.MaxInt},
			},
			wantStateName: "github.com/gostdlib/base/statemachine.steer",
			wantRequest:   Request[data]{Ctx: parentCtx, Data: data{Num: math.MaxInt}, Next: addErr},
		},
		{
			name: "Route to nil",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: steer,
				Data: data{Num: 0},
			},
			wantStateName: "github.com/gostdlib/base/statemachine.steer",
			wantRequest:   Request[data]{Ctx: parentCtx, Data: data{Num: 0}, Next: nil},
		},
		{
			name: "Check data change in addTen",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: addTen,
				Data: data{Num: 1},
			},
			wantStateName: "github.com/gostdlib/base/statemachine.addTen",
			wantRequest:   Request[data]{Ctx: parentCtx, Data: data{Num: 11}, Next: nil},
		},
		{
			name: "Check error in addErr",
			req: Request[data]{
				Ctx:  parentCtx,
				Next: addErr,
				Data: data{Num: 1},
			},
			wantStateName: "github.com/gostdlib/base/statemachine.addErr",
			wantRequest:   Request[data]{Ctx: parentCtx, Data: data{Num: 1}, Err: fmt.Errorf("addErr")},
		},
	}

	for _, test := range tests {
		gotStateName, gotRequest := execState(test.req)
		if gotStateName != test.wantStateName {
			t.Errorf("TestExecState(%s): stateName: got %q, want %q", test.name, gotStateName, test.wantStateName)
		}
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
