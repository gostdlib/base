package stream

import (
	"testing"

	"github.com/gostdlib/base/context"
	grpcContext "github.com/gostdlib/base/context/grpc"
	"github.com/gostdlib/base/errors"
	pb "github.com/gostdlib/base/errors/example/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
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
			name:        "Intercept send error",
			intercepts:  []Intercept{&testIntercept{sendErr: errors.New("send error")}},
			wantSendErr: true,
			customerID:  "12345",
			incomingMD:  metadata.Pairs("customerID", "12345"),
		},
		{
			name:        "Intercept recv error",
			intercepts:  []Intercept{&testIntercept{recvErr: errors.New("recv error")}},
			wantRecvErr: true,
			customerID:  "12345",
			incomingMD:  metadata.Pairs("customerID", "12345"),
		},
		{
			name:       "Successful send and recv",
			intercepts: []Intercept{&testIntercept{}},
			customerID: "12345",
			incomingMD: metadata.Pairs("customerID", "12345"),
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
