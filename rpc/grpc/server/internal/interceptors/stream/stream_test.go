package stream

import (
	"fmt"
	"testing"

	"github.com/gostdlib/base/context"
	grpcContext "github.com/gostdlib/base/context/grpc"
	"github.com/gostdlib/base/errors"
	pb "github.com/gostdlib/base/errors/example/proto"

	"google.golang.org/grpc"
	codespkg "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type fakeServerStream struct {
	grpc.ServerStream
	sendMsg func(m interface{}) error
	recvMsg func(m interface{}) error
	ctx     context.Context
}

func (m *fakeServerStream) SendMsg(msg interface{}) error {
	return m.sendMsg(msg)
}

func (m *fakeServerStream) RecvMsg(msg interface{}) error {
	return m.recvMsg(msg)
}

func (m *fakeServerStream) Context() context.Context {
	return m.ctx
}

type testIntercept struct {
	sendErr error
	recvErr error
}

func (m *testIntercept) Send(ctx context.Context, md grpcContext.Metadata, ss grpc.ServerStream, msg any) error {
	return m.sendErr
}

func (m *testIntercept) Recv(ctx context.Context, md grpcContext.Metadata, msg any) error {
	return m.recvErr
}

func TestNewAssignsErrConvert(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	convert := func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error) {
		return status.New(codespkg.Internal, e.Error()), nil
	}

	interceptor, err := New(ctx, convert)
	if err != nil {
		t.Fatalf("TestNewAssignsErrConvert: got err == %s, want err == nil", err)
	}

	if interceptor.errConvert == nil {
		t.Errorf("TestNewAssignsErrConvert: errConvert is nil, want non-nil (parameter was ignored)")
	}
}

func TestStreamInterceptor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		intercepts  []Intercept
		sendErr     error
		recvErr     error
		wantSendErr bool
		wantRecvErr bool
		customerID  string
		incomingMD  metadata.MD
	}{
		{
			name:        "Error: intercept send error",
			intercepts:  []Intercept{&testIntercept{sendErr: errors.New("send error")}},
			wantSendErr: true,
			customerID:  "12345",
			incomingMD:  metadata.Pairs("CustomerID", "12345"),
		},
		{
			name:        "Error: intercept recv error",
			intercepts:  []Intercept{&testIntercept{recvErr: errors.New("recv error")}},
			wantRecvErr: true,
			customerID:  "12345",
			incomingMD:  metadata.Pairs("CustomerID", "12345"),
		},
		{
			name:       "Success: send and recv",
			intercepts: []Intercept{&testIntercept{}},
			customerID: "12345",
			incomingMD: metadata.Pairs("CustomerID", "12345"),
		},
		{
			name:       "Success: no metadata",
			intercepts: []Intercept{&testIntercept{}},
			customerID: "",
			incomingMD: metadata.MD{},
		},
	}

	for _, test := range tests {
		ctx := metadata.NewIncomingContext(context.Background(), test.incomingMD)

		fss := &fakeServerStream{
			ctx: ctx,
			sendMsg: func(m interface{}) error {
				return test.sendErr
			},
			recvMsg: func(m interface{}) error {
				return test.recvErr
			},
		}

		interceptor, err := New(ctx, nil, WithIntercepts(test.intercepts...))
		if err != nil {
			t.Fatalf("Failed to create interceptor: %v", err)
		}

		handler := func(srv interface{}, stream grpc.ServerStream) error {
			msg := &pb.HelloReq{Name: "test"}

			err := stream.SendMsg(msg)
			switch {
			case err == nil && test.wantSendErr:
				t.Errorf("TestStreamInterceptor(%s)(send): got err == nil, want err != nil", test.name)
				return nil
			case err != nil && !test.wantSendErr:
				t.Errorf("TestStreamInterceptor(%s)(send): got err != nil, want err == nil", test.name)
				return nil
			}

			err = stream.RecvMsg(msg)
			switch {
			case err == nil && test.wantRecvErr:
				t.Errorf("TestStreamInterceptor(%s)(recv): got err == nil, want err != nil", test.name)
				return nil
			case err != nil && !test.wantRecvErr:
				t.Errorf("TestStreamInterceptor(%s)(recv): got err != nil, want err == nil", test.name)
				return nil
			}
			return nil
		}

		err = interceptor.Intercept(
			nil,
			fss,
			&grpc.StreamServerInfo{FullMethod: "/service/method"},
			handler,
		)
		if err != nil && !(test.wantSendErr || test.wantRecvErr) {
			t.Errorf("TestStreamInterceptor(%s): got err != nil, want err == nil", test.name)
		}
	}
}

func TestInterceptMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		incomingMD     metadata.MD
		wantCustomerID string
	}{
		{
			name:           "Success: CustomerID extracted from metadata",
			incomingMD:     metadata.Pairs("CustomerID", "cust-42"),
			wantCustomerID: "cust-42",
		},
		{
			name:           "Success: no CustomerID in metadata",
			incomingMD:     metadata.MD{},
			wantCustomerID: "",
		},
		{
			name:           "Success: CallID overridden by CustomerID",
			incomingMD:     metadata.Pairs("CallID", "call-1", "CustomerID", "cust-99"),
			wantCustomerID: "cust-99",
		},
		{
			name:           "Success: CallID used when no CustomerID",
			incomingMD:     metadata.Pairs("CallID", "call-1"),
			wantCustomerID: "call-1",
		},
	}

	for _, test := range tests {
		var capturedMD grpcContext.Metadata

		ctx := metadata.NewIncomingContext(context.Background(), test.incomingMD)
		fss := &fakeServerStream{
			ctx:     ctx,
			sendMsg: func(m interface{}) error { return nil },
			recvMsg: func(m interface{}) error { return nil },
		}

		intercept := &metadataCapture{captured: &capturedMD}
		interceptor, err := New(ctx, nil, WithIntercepts(intercept))
		if err != nil {
			t.Fatalf("TestInterceptMetadata(%s): got err == %s, want err == nil", test.name, err)
		}

		handler := func(srv interface{}, stream grpc.ServerStream) error {
			msg := &pb.HelloReq{Name: "test"}
			return stream.SendMsg(msg)
		}

		err = interceptor.Intercept(nil, fss, &grpc.StreamServerInfo{FullMethod: "/service/method"}, handler)
		if err != nil {
			t.Errorf("TestInterceptMetadata(%s): got err == %s, want err == nil", test.name, err)
			continue
		}

		if capturedMD.CustomerID != test.wantCustomerID {
			t.Errorf("TestInterceptMetadata(%s): CustomerID: got %q, want %q", test.name, capturedMD.CustomerID, test.wantCustomerID)
		}
		if capturedMD.Op != "/service/method" {
			t.Errorf("TestInterceptMetadata(%s): Op: got %q, want %q", test.name, capturedMD.Op, "/service/method")
		}
		if capturedMD.CallID == "" {
			t.Errorf("TestInterceptMetadata(%s): CallID: got empty, want non-empty", test.name)
		}
	}
}

type metadataCapture struct {
	captured *grpcContext.Metadata
}

func (m *metadataCapture) Send(ctx context.Context, md grpcContext.Metadata, ss grpc.ServerStream, msg any) error {
	*m.captured = md
	return nil
}

func (m *metadataCapture) Recv(ctx context.Context, md grpcContext.Metadata, msg any) error {
	*m.captured = md
	return nil
}

func TestStreamErrLogAndConvert(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sendErr    error
		errConvert ErrConvert
		wantErr    bool
	}{
		{
			name:    "Error: errors.Error without converter",
			sendErr: errors.New("send failed"),
			wantErr: true,
		},
		{
			name:    "Error: errors.Error with converter",
			sendErr: errors.New("send failed"),
			errConvert: func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error) {
				return status.New(codespkg.Internal, e.Error()), nil
			},
			wantErr: true,
		},
		{
			name:    "Error: errors.Error with failing converter",
			sendErr: errors.New("send failed"),
			errConvert: func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error) {
				return nil, fmt.Errorf("conversion failed")
			},
			wantErr: true,
		},
		{
			name:    "Error: standard error without converter",
			sendErr: fmt.Errorf("plain error"),
			wantErr: true,
		},
		{
			name:    "Error: standard error with converter",
			sendErr: fmt.Errorf("plain error"),
			errConvert: func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error) {
				return status.New(codespkg.Unknown, e.Error()), nil
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("CustomerID", "12345"))
		fss := &fakeServerStream{
			ctx: ctx,
			sendMsg: func(m interface{}) error {
				return test.sendErr
			},
			recvMsg: func(m interface{}) error { return nil },
		}

		interceptor, err := New(ctx, test.errConvert)
		if err != nil {
			t.Fatalf("TestStreamErrLogAndConvert(%s): got err == %s, want err == nil", test.name, err)
		}

		handler := func(srv interface{}, stream grpc.ServerStream) error {
			return stream.SendMsg(&pb.HelloReq{Name: "test"})
		}

		err = interceptor.Intercept(nil, fss, &grpc.StreamServerInfo{FullMethod: "/service/method"}, handler)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestStreamErrLogAndConvert(%s): got err == nil, want err != nil", test.name)
		case err != nil && !test.wantErr:
			t.Errorf("TestStreamErrLogAndConvert(%s): got err == %s, want err == nil", test.name, err)
		}
	}
}

func TestStreamMultipleIntercepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		intercepts  []Intercept
		wantSendErr bool
		wantRecvErr bool
	}{
		{
			name: "Success: multiple intercepts all pass",
			intercepts: []Intercept{
				&testIntercept{},
				&testIntercept{},
				&testIntercept{},
			},
		},
		{
			name: "Error: second send intercept fails",
			intercepts: []Intercept{
				&testIntercept{},
				&testIntercept{sendErr: errors.New("second fails")},
				&testIntercept{},
			},
			wantSendErr: true,
		},
		{
			name: "Error: second recv intercept fails",
			intercepts: []Intercept{
				&testIntercept{},
				&testIntercept{recvErr: errors.New("second fails")},
				&testIntercept{},
			},
			wantRecvErr: true,
		},
	}

	for _, test := range tests {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("CustomerID", "12345"))
		fss := &fakeServerStream{
			ctx:     ctx,
			sendMsg: func(m interface{}) error { return nil },
			recvMsg: func(m interface{}) error { return nil },
		}

		interceptor, err := New(ctx, nil, WithIntercepts(test.intercepts...))
		if err != nil {
			t.Fatalf("TestStreamMultipleIntercepts(%s): got err == %s, want err == nil", test.name, err)
		}

		handler := func(srv any, stream grpc.ServerStream) error {
			msg := &pb.HelloReq{Name: "test"}

			err := stream.SendMsg(msg)
			switch {
			case err == nil && test.wantSendErr:
				t.Errorf("TestStreamMultipleIntercepts(%s)(send): got err == nil, want err != nil", test.name)
				return nil
			case err != nil && !test.wantSendErr:
				t.Errorf("TestStreamMultipleIntercepts(%s)(send): got err == %s, want err == nil", test.name, err)
				return nil
			case err != nil:
				return nil
			}

			err = stream.RecvMsg(msg)
			switch {
			case err == nil && test.wantRecvErr:
				t.Errorf("TestStreamMultipleIntercepts(%s)(recv): got err == nil, want err != nil", test.name)
			case err != nil && !test.wantRecvErr:
				t.Errorf("TestStreamMultipleIntercepts(%s)(recv): got err == %s, want err == nil", test.name, err)
			}
			return nil
		}

		err = interceptor.Intercept(nil, fss, &grpc.StreamServerInfo{FullMethod: "/service/method"}, handler)
		if err != nil && !(test.wantSendErr || test.wantRecvErr) {
			t.Errorf("TestStreamMultipleIntercepts(%s): got err != nil, want err == nil", test.name)
		}
	}
}

func TestStreamRecvMsgError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		recvErr    error
		errConvert ErrConvert
		wantErr    bool
	}{
		{
			name:    "Error: underlying stream recv fails",
			recvErr: fmt.Errorf("stream recv error"),
			wantErr: true,
		},
		{
			name:    "Error: underlying stream recv fails with converter",
			recvErr: fmt.Errorf("stream recv error"),
			errConvert: func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error) {
				return status.New(codespkg.Unavailable, e.Error()), nil
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("CustomerID", "12345"))
		fss := &fakeServerStream{
			ctx:     ctx,
			sendMsg: func(m any) error { return nil },
			recvMsg: func(m any) error { return test.recvErr },
		}

		interceptor, err := New(ctx, test.errConvert)
		if err != nil {
			t.Fatalf("TestStreamRecvMsgError(%s): got err == %s, want err == nil", test.name, err)
		}

		handler := func(srv any, stream grpc.ServerStream) error {
			msg := &pb.HelloReq{Name: "test"}
			return stream.RecvMsg(msg)
		}

		err = interceptor.Intercept(nil, fss, &grpc.StreamServerInfo{FullMethod: "/service/method"}, handler)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestStreamRecvMsgError(%s): got err == nil, want err != nil", test.name)
		case err != nil && !test.wantErr:
			t.Errorf("TestStreamRecvMsgError(%s): got err == %s, want err == nil", test.name, err)
		}
	}
}
