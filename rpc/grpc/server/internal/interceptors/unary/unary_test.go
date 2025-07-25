package unary

import (
	stdErr "errors"
	"fmt"
	"testing"

	"github.com/gostdlib/base/context"
	grpcContext "github.com/gostdlib/base/context/grpc"
	"github.com/gostdlib/base/errors"
	pb "github.com/gostdlib/base/errors/example/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestIntercept(t *testing.T) {
	t.Parallel()

	var resultCtx context.Context

	tests := []struct {
		name       string
		ctx        context.Context
		handler    grpc.UnaryHandler
		wantErr    error
		customerID string
	}{
		{
			name: "Valid metadata and successful handler",
			ctx:  metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			handler: func(ctx context.Context, req any) (any, error) {
				resultCtx = ctx
				return "response", nil
			},
			wantErr:    nil,
			customerID: "12345",
		},
		{
			name: "No metadata",
			ctx:  context.Background(),
			handler: func(ctx context.Context, req any) (any, error) {
				resultCtx = ctx
				return "response", nil
			},
			wantErr:    nil,
			customerID: "",
		},
		{
			name: "Handler returns error",
			ctx:  metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			handler: func(ctx context.Context, req any) (any, error) {
				resultCtx = ctx
				return nil, status.Error(13, "handler error")
			},
			wantErr:    status.Error(13, "handler error"),
			customerID: "12345",
		},
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/service/method",
	}
	req := &pb.HelloReq{Name: "world"}

	for _, test := range tests {
		unary := &Interceptor{}

		_, err := unary.Intercept(test.ctx, req, info, test.handler)

		if test.wantErr != nil {
			if err == nil || err.Error() != test.wantErr.Error() {
				t.Errorf("TestUnaryIntercept(%s): expected error %v, got %v", test.name, test.wantErr, err)
			}
		} else if err != nil {
			t.Errorf("TestUnaryIntercept(%s): did not expect error, got %v", test.name, err)
		}
		if err != nil {
			if !stdErr.Is(err, errors.Error{}) {
				t.Errorf("TestUnaryIntercept(%s): expected error to be of type errors.Error, got %T", test.name, err)
			}
		}

		gMeta := grpcContext.GetMetadata(resultCtx)
		if test.customerID != gMeta.CustomerID {
			t.Errorf("TestUnaryIntercept(%s): customerID: got %v, want %v", test.name, gMeta.CustomerID, test.customerID)
		}
		if gMeta.Op != info.FullMethod {
			t.Errorf("TestUnaryIntercept(%s): method: got %v, want %v", test.name, gMeta.Op, info.FullMethod)
		}
		if gMeta.CallID == "" {
			t.Errorf("TestUnaryIntercept(%s): callID: got empty, want non-empty", test.name)
		}

	}
}

// testIntercept is a test implementation of the Intercept function
type testIntercept struct {
	returnErr    error
	modifyReq    bool
	called       bool
	receivedReq  any
	receivedMeta grpcContext.Metadata
}

func (t *testIntercept) intercept(ctx context.Context, req any, md grpcContext.Metadata) (any, error) {
	t.called = true
	t.receivedReq = req
	t.receivedMeta = md

	if t.returnErr != nil {
		return nil, t.returnErr
	}

	if t.modifyReq {
		// Modify the request to test that changes are passed through
		if helloReq, ok := req.(*pb.HelloReq); ok {
			return &pb.HelloReq{Name: helloReq.Name + "_modified"}, nil
		}
	}

	return req, nil
}

func TestWithIntercept(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		intercepts      []*testIntercept
		ctx             context.Context
		wantErr         bool
		customerID      string
		expectAllCalled bool
	}{
		{
			name: "Single successful intercept",
			intercepts: []*testIntercept{
				{returnErr: nil},
			},
			ctx:             metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			wantErr:         false,
			customerID:      "12345",
			expectAllCalled: true,
		},
		{
			name: "Multiple successful intercepts",
			intercepts: []*testIntercept{
				{returnErr: nil},
				{returnErr: nil},
				{returnErr: nil},
			},
			ctx:             metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			wantErr:         false,
			customerID:      "12345",
			expectAllCalled: true,
		},
		{
			name: "First intercept returns error",
			intercepts: []*testIntercept{
				{returnErr: errors.New("intercept error")},
				{returnErr: nil}, // This should not be called
			},
			ctx:             metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			wantErr:         true,
			customerID:      "12345",
			expectAllCalled: false,
		},
		{
			name: "Second intercept returns error",
			intercepts: []*testIntercept{
				{returnErr: nil},
				{returnErr: errors.New("second intercept error")},
				{returnErr: nil}, // This should not be called
			},
			ctx:             metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			wantErr:         true,
			customerID:      "12345",
			expectAllCalled: false,
		},
		{
			name: "Intercept modifies request",
			intercepts: []*testIntercept{
				{modifyReq: true},
			},
			ctx:             metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			wantErr:         false,
			customerID:      "12345",
			expectAllCalled: true,
		},
		{
			name: "No metadata",
			intercepts: []*testIntercept{
				{returnErr: nil},
			},
			ctx:             context.Background(),
			wantErr:         false,
			customerID:      "",
			expectAllCalled: true,
		},
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/service/method",
	}
	req := &pb.HelloReq{Name: "world"}

	// Simple handler that just returns success
	handler := func(ctx context.Context, req any) (any, error) {
		return "response", nil
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Convert test intercepts to Intercept functions
			intercepts := make([]Intercept, len(test.intercepts))
			for i, ti := range test.intercepts {
				intercepts[i] = ti.intercept
			}

			// Create interceptor with intercepts
			unary, err := New(context.Background(), nil, WithIntercept(intercepts...))
			if err != nil {
				t.Fatalf("Failed to create unary interceptor: %v", err)
			}

			// Call the interceptor
			_, err = unary.Intercept(test.ctx, req, info, handler)

			// Check error expectation
			if test.wantErr && err == nil {
				t.Errorf("Expected error but got none")
			} else if !test.wantErr && err != nil {
				t.Errorf("Did not expect error but got: %v", err)
			}

			// Check that intercepts were called as expected
			calledCount := 0
			for i, ti := range test.intercepts {
				if ti.called {
					calledCount++
					// Check that metadata was passed correctly
					if ti.receivedMeta.CustomerID != test.customerID {
						t.Errorf("Intercept %d: expected customerID %s, got %s", i, test.customerID, ti.receivedMeta.CustomerID)
					}
					if ti.receivedMeta.Op != info.FullMethod {
						t.Errorf("Intercept %d: expected method %s, got %s", i, info.FullMethod, ti.receivedMeta.Op)
					}
					if ti.receivedMeta.CallID == "" {
						t.Errorf("Intercept %d: expected non-empty callID", i)
					}
				} else if test.expectAllCalled {
					t.Errorf("Intercept %d was not called but should have been", i)
				}

				// If this intercept returns an error, no subsequent intercepts should be called
				if ti.returnErr != nil {
					break
				}
			}

			// Verify expected call count
			if test.expectAllCalled {
				if calledCount != len(test.intercepts) {
					t.Errorf("Expected all %d intercepts to be called, but only %d were called", len(test.intercepts), calledCount)
				}
			}
		})
	}
}

func TestWithInterceptRequestModification(t *testing.T) {
	t.Parallel()

	var finalReq any

	// Handler captures the final request to verify modifications
	handler := func(ctx context.Context, req any) (any, error) {
		finalReq = req
		return "response", nil
	}

	tests := []struct {
		name            string
		intercepts      []*testIntercept
		originalReqName string
		expectedReqName string
	}{
		{
			name: "Single modification",
			intercepts: []*testIntercept{
				{modifyReq: true},
			},
			originalReqName: "world",
			expectedReqName: "world_modified",
		},
		{
			name: "Chain of modifications",
			intercepts: []*testIntercept{
				{modifyReq: true}, // world -> world_modified
				{modifyReq: true}, // world_modified -> world_modified_modified
			},
			originalReqName: "world",
			expectedReqName: "world_modified_modified",
		},
		{
			name: "No modification",
			intercepts: []*testIntercept{
				{modifyReq: false},
			},
			originalReqName: "world",
			expectedReqName: "world",
		},
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/service/method"}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345"))

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := &pb.HelloReq{Name: test.originalReqName}

			// Convert test intercepts to Intercept functions
			intercepts := make([]Intercept, len(test.intercepts))
			for i, ti := range test.intercepts {
				intercepts[i] = ti.intercept
			}

			// Create interceptor with intercepts
			unary, err := New(context.Background(), nil, WithIntercept(intercepts...))
			if err != nil {
				t.Fatalf("Failed to create unary interceptor: %v", err)
			}

			// Call the interceptor
			_, err = unary.Intercept(ctx, req, info, handler)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Check that the request was modified as expected
			if helloReq, ok := finalReq.(*pb.HelloReq); ok {
				if helloReq.Name != test.expectedReqName {
					t.Errorf("Expected final request name '%s', got '%s'", test.expectedReqName, helloReq.Name)
				}
			} else {
				t.Errorf("Expected final request to be *pb.HelloReq, got %T", finalReq)
			}
		})
	}
}

func TestWithInterceptNilIntercept(t *testing.T) {
	t.Parallel()

	// Test that WithIntercept rejects nil intercepts
	_, err := New(context.Background(), nil, WithIntercept(nil))
	if err == nil {
		t.Error("Expected error when passing nil intercept, got none")
	}

	// Test that WithIntercept rejects intercepts that include nil
	validIntercept := func(ctx context.Context, req any, md grpcContext.Metadata) (any, error) {
		return req, nil
	}
	_, err = New(context.Background(), nil, WithIntercept(validIntercept, nil))
	if err == nil {
		t.Error("Expected error when passing nil intercept in list, got none")
	}
}

func TestWithInterceptErrorConversion(t *testing.T) {
	t.Parallel()

	customErr := errors.E(t.Context(), errors.CatReq, errors.TypeBadRequest, fmt.Errorf("custom intercept error"))

	testErrConvert := func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error) {
		return status.New(13, "converted error"), nil
	}

	intercept := func(ctx context.Context, req any, md grpcContext.Metadata) (any, error) {
		return nil, customErr
	}

	unary, err := New(context.Background(), testErrConvert, WithIntercept(intercept))
	if err != nil {
		t.Fatalf("Failed to create unary interceptor: %v", err)
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/service/method"}
	req := &pb.HelloReq{Name: "world"}
	ctx := context.Background()

	handler := func(ctx context.Context, req any) (any, error) {
		return "response", nil
	}

	_, err = unary.Intercept(ctx, req, info, handler)

	if err == nil {
		t.Error("Expected error from failed intercept")
		return
	}

	// Check that the error was converted
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error after conversion: %s", err)
	}

	if st.Code() != 13 {
		t.Errorf("Expected status code 13, got %d", st.Code())
	}
	if st.Message() != "converted error" {
		t.Errorf("Expected message 'converted error', got '%s'", st.Message())
	}
}
