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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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
		unary, err := New(t.Context(), nil)
		if err != nil {
			t.Errorf("TestUnaryIntercept(%s): Failed to create unary interceptor: %v", test.name, err)
			continue
		}

		_, err = unary.Intercept(test.ctx, req, info, test.handler)

		switch {
		case err == nil && test.wantErr != nil:
			t.Errorf("TestUnaryIntercept(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && test.wantErr == nil:
			t.Errorf("TestUnaryIntercept(%s): got err != nil, want err == nil", test.name)
			continue
		}
		if err != nil {
			if !stdErr.Is(err, errors.Error{}) {
				t.Errorf("TestUnaryIntercept(%s): expected error to be of type errors.Error, got %T", test.name, err)
				continue
			}
			if err.Error() != test.wantErr.Error() {
				t.Errorf("TestUnaryIntercept(%s): expected error %v, got %v", test.name, test.wantErr, err)
			}
			continue
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

func (t *testIntercept) intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	t.called = true
	t.receivedReq = req
	md := grpcContext.GetMetadata(ctx)
	t.receivedMeta = md

	if t.returnErr != nil {
		return nil, t.returnErr
	}

	if t.modifyReq {
		// Modify the request to test that changes are passed through
		if helloReq, ok := req.(*pb.HelloReq); ok {
			return handler(ctx, &pb.HelloReq{Name: helloReq.Name + "_modified"})
		}
	}

	return handler(ctx, req)
}

func TestWithIntercept(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		intercepts   []*testIntercept
		ctx          context.Context
		wantErr      bool
		customerID   string
		expectCalled int
	}{
		{
			name: "Single successful intercept",
			intercepts: []*testIntercept{
				{returnErr: nil},
			},
			ctx:          metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			wantErr:      false,
			customerID:   "12345",
			expectCalled: 1,
		},
		{
			name: "Multiple successful intercepts",
			intercepts: []*testIntercept{
				{returnErr: nil},
				{returnErr: nil},
				{returnErr: nil},
			},
			ctx:          metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			wantErr:      false,
			customerID:   "12345",
			expectCalled: 3,
		},
		{
			name: "First intercept returns error",
			intercepts: []*testIntercept{
				{returnErr: errors.New("intercept error")},
				{returnErr: nil}, // This should not be called
			},
			ctx:          metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			wantErr:      true,
			customerID:   "12345",
			expectCalled: 1,
		},
		{
			name: "Second intercept returns error",
			intercepts: []*testIntercept{
				{returnErr: nil},
				{returnErr: errors.New("second intercept error")},
				{returnErr: nil}, // This should not be called
			},
			ctx:          metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			wantErr:      true,
			customerID:   "12345",
			expectCalled: 2,
		},
		{
			name: "Intercept modifies request",
			intercepts: []*testIntercept{
				{modifyReq: true},
			},
			ctx:          metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			wantErr:      false,
			customerID:   "12345",
			expectCalled: 1,
		},
		{
			name: "No metadata",
			intercepts: []*testIntercept{
				{returnErr: nil},
			},
			ctx:          context.Background(),
			wantErr:      false,
			customerID:   "",
			expectCalled: 1,
		},
		{
			name:         "Empty intercepts",
			intercepts:   nil,
			ctx:          metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345")),
			wantErr:      false,
			customerID:   "12345",
			expectCalled: 0,
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
		intercepts := make([]grpc.UnaryServerInterceptor, len(test.intercepts))
		for i, ti := range test.intercepts {
			intercepts[i] = ti.intercept
		}

		unary, err := New(context.Background(), nil, WithIntercept(intercepts...))
		if err != nil {
			t.Errorf("TestWithIntercept(%s): Failed to create unary interceptor: %v", test.name, err)
			continue
		}

		_, err = unary.Intercept(test.ctx, req, info, handler)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestWithIntercept(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestWithIntercept(%s): got err != nil, want err == nil", test.name)
			continue
		}

		calledCount := 0
		for i, ti := range test.intercepts {
			if ti.called {
				calledCount++
				if ti.receivedMeta.CustomerID != test.customerID {
					t.Errorf("TestWithIntercept(%s): Intercept %d: expected customerID %s, got %s", test.name, i, test.customerID, ti.receivedMeta.CustomerID)
				}
				if ti.receivedMeta.Op != info.FullMethod {
					t.Errorf("TestWithIntercept(%s): Intercept %d: expected method %s, got %s", test.name, i, info.FullMethod, ti.receivedMeta.Op)
				}
				if ti.receivedMeta.CallID == "" {
					t.Errorf("TestWithIntercept(%s): Intercept %d: expected non-empty callID", test.name, i)
				}
			}
		}

		if calledCount != test.expectCalled {
			t.Errorf("TestWithIntercept(%s): Expected %d intercepts to be called, but %d were called", test.name, test.expectCalled, calledCount)
		}
	}
}

func TestWithInterceptRequestModification(t *testing.T) {
	t.Parallel()

	var finalReq any

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
		req := &pb.HelloReq{Name: test.originalReqName}

		intercepts := make([]grpc.UnaryServerInterceptor, len(test.intercepts))
		for i, ti := range test.intercepts {
			intercepts[i] = ti.intercept
		}

		unary, err := New(context.Background(), nil, WithIntercept(intercepts...))
		if err != nil {
			t.Errorf("TestWithInterceptRequestModification(%s): Failed to create unary interceptor: %v", test.name, err)
			continue
		}

		_, err = unary.Intercept(ctx, req, info, handler)
		if err != nil {
			t.Errorf("TestWithInterceptRequestModification(%s): Unexpected error: %v", test.name, err)
			continue
		}

		if helloReq, ok := finalReq.(*pb.HelloReq); ok {
			if helloReq.Name != test.expectedReqName {
				t.Errorf("TestWithInterceptRequestModification(%s): Expected final request name '%s', got '%s'", test.name, test.expectedReqName, helloReq.Name)
			}
		} else {
			t.Errorf("TestWithInterceptRequestModification(%s): Expected final request to be *pb.HelloReq, got %T", test.name, finalReq)
		}
	}
}

func TestWithInterceptNilIntercept(t *testing.T) {
	t.Parallel()

	// Test that WithIntercept rejects nil intercepts
	_, err := New(context.Background(), nil, WithIntercept(nil))
	if err == nil {
		t.Error("Expected error when passing nil intercept, got none")
	}

	validIntercept := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		return handler(ctx, req)
	}

	_, err = New(context.Background(), nil, WithIntercept(validIntercept, nil))
	if err == nil {
		t.Error("Expected error when passing nil intercept in list, got none")
	}
}

func TestWithInterceptErrorConversion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		errConvert  ErrConvert
		wantStatus  bool
		wantCode    uint32
		wantMessage string
		wantErr     bool
	}{
		{
			name: "Custom errors.Error with conversion using error details",
			err:  errors.E(context.Background(), CatReq, TypeBadRequest, fmt.Errorf("custom intercept error")),
			errConvert: func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error) {
				return status.New(codes.Internal, fmt.Sprintf("converted: %s for %s", e.Error(), meta.Op)), nil
			},
			wantStatus:  true,
			wantCode:    uint32(codes.Internal),
			wantMessage: "converted: custom intercept error for /service/method",
			wantErr:     true,
		},
		{
			name: "Custom errors.Error with conversion failure",
			err:  errors.E(context.Background(), CatReq, TypeBadRequest, fmt.Errorf("custom intercept error")),
			errConvert: func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error) {
				return nil, fmt.Errorf("conversion failed for error: %s", e.Error())
			},
			wantStatus:  false,
			wantCode:    0,
			wantMessage: "custom intercept error",
			wantErr:     true,
		},
		{
			name: "Standard error",
			err:  fmt.Errorf("standard error"),
			errConvert: func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error) {
				return status.New(codes.Unknown, fmt.Sprintf("wrapped error: %s", e.Error())), nil
			},
			wantStatus:  false,
			wantMessage: "standard error",
			wantErr:     true,
		},
		{
			name:        "Custom errors.Error without converter",
			err:         errors.E(context.Background(), CatReq, TypeBadRequest, fmt.Errorf("custom intercept error")),
			errConvert:  nil,
			wantStatus:  false,
			wantCode:    0,
			wantMessage: "custom intercept error",
			wantErr:     true,
		},
		{
			name:        "Standard error without converter",
			err:         fmt.Errorf("standard error"),
			errConvert:  nil,
			wantStatus:  false,
			wantCode:    0,
			wantMessage: "standard error",
			wantErr:     true,
		},
		{
			name: "Custom errors.Error with metadata-based conversion",
			err:  errors.E(context.Background(), CatReq, TypeBadRequest, fmt.Errorf("access denied")),
			errConvert: func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error) {
				// Use metadata to customize the error response
				code := codes.PermissionDenied
				if meta.CustomerID != "" {
					return status.New(code, fmt.Sprintf("Customer %s: %s", meta.CustomerID, e.Error())), nil
				}
				return status.New(code, e.Error()), nil
			},
			wantStatus:  true,
			wantCode:    uint32(codes.PermissionDenied),
			wantMessage: "Customer 12345: access denied",
			wantErr:     true,
		},
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/service/method"}
	req := &pb.HelloReq{Name: "world"}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("customerID", "12345"))

	handler := func(ctx context.Context, req any) (any, error) {
		return "response", nil
	}

	for _, test := range tests {
		intercept := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
			if test.err != nil {
				return nil, test.err
			}
			return handler(ctx, req)
		}

		unary, err := New(context.Background(), test.errConvert, WithIntercept(intercept))
		if err != nil {
			t.Errorf("TestWithInterceptErrorConversion(%s): Failed to create unary interceptor: %v", test.name, err)
			continue
		}

		_, err = unary.Intercept(ctx, req, info, handler)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestWithInterceptErrorConversion(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestWithInterceptErrorConversion(%s): got err != nil, want err == nil", test.name)
			continue
		}

		if test.wantStatus {
			st, ok := status.FromError(err)
			if !ok {
				t.Errorf("TestWithInterceptErrorConversion(%s): Expected gRPC status error after conversion, got: %T", test.name, err)
				continue
			}

			if uint32(st.Code()) != test.wantCode {
				t.Errorf("TestWithInterceptErrorConversion(%s): Expected status code %d, got %d", test.name, test.wantCode, st.Code())
			}
			if st.Message() != test.wantMessage {
				t.Errorf("TestWithInterceptErrorConversion(%s): Expected message '%s', got '%s'", test.name, test.wantMessage, st.Message())
			}
			continue
		}

		if err.Error() != test.wantMessage {
			t.Errorf("TestWithInterceptErrorConversion(%s): Expected error message '%s', got '%s'", test.name, test.wantMessage, err.Error())
		}
	}
}
