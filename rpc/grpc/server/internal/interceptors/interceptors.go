// Package interceptors provides gRPC interceptors for handling gRPC calls.
package interceptors

import (
	"github.com/gostdlib/base/context"
	grpcContext "github.com/gostdlib/base/context/grpc"
	"github.com/gostdlib/base/errors"
	"github.com/gostdlib/base/telemetry/otel/trace/span"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Unary provide a unary interceptor for gRPC.
type Unary struct {
	// ErrConvert is a function that converts an error to a gRPC status.
	ErrConvert  func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error)
	spanOptions []span.Option
}

// Intercept intercepts unary gRPC calls.
func (u *Unary) Intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	// Attach our relevant clients.
	ctx = context.Attach(ctx)

	callID := mustUUID().String()
	customerID := ""
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		id := md.Get("customerID")
		if len(id) == 1 {
			customerID = id[0]
		}
	}

	grpcMeta := grpcContext.Metadata{
		CallID:     callID,
		CustomerID: customerID,
		Op:         info.FullMethod,
	}
	ctx = grpcContext.SetMetadata(ctx, grpcMeta)

	// Add our trace span.
	opts := make([]span.Option, 0, len(u.spanOptions)+1)
	opts = append(opts, span.WithName(info.FullMethod))
	opts = append(opts, u.spanOptions...)
	ctx, spanner := span.New(ctx, opts...)
	defer spanner.End()

	resp, err := handler(ctx, req)
	if err != nil {
		if e, ok := err.(errors.Error); ok {
			e.Log(ctx, callID, customerID, req)
			if u.ErrConvert != nil {
				status, err := u.ErrConvert(ctx, e, grpcMeta)
				if err != nil {
					return nil, err
				}
				return nil, status.Err()
			}
			return nil, e
		} else {
			e := errors.E(ctx, nil, nil, err)
			e.Log(ctx, callID, customerID, req)
			return nil, e
		}
	}

	return resp, err
}

// mustUUID generates a new UUID v7. If it fails, it panics.
// UUID generation can only fail if something is terribly wrong. Maybe a system clock is
// gravely out of sync or you are doing something wrong with the randomization pool. But if
// either of these are the case, you can't generate unique IDs. If you can't generate unique IDs,
// then you have panic level problems, hence the panic.
func mustUUID() uuid.UUID {
	u, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return u
}
