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
	default:
		req.Next = addTen
	}
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
	}

	for _, test := range tests {
		gotReq, err := Run(test.argName, test.req)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestRun(%s) got err == nil, want err != nil", test.name)
		case err != nil && !test.wantErr:
			t.Errorf("TestRun(%s) got err == %s, want err == nil", test.name, err)
		}
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
