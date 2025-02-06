package interceptors

import (
	stdErr "errors"
	"testing"

	"github.com/gostdlib/base/context"
	grpcContext "github.com/gostdlib/base/context/grpc"
	"github.com/gostdlib/base/errors"
	pb "github.com/gostdlib/base/errors/example/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryIntercept(t *testing.T) {
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
		unary := &Unary{}

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
