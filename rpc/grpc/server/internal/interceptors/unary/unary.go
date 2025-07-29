package unary

import (
	"github.com/google/uuid"
	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/errors"
	"github.com/gostdlib/base/telemetry/otel/trace/span"

	grpcContext "github.com/gostdlib/base/context/grpc"
	middle "github.com/grpc-ecosystem/go-grpc-middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ErrConvert is a function that converts an error to a gRPC status. If it cannot, it returns a standard error.
type ErrConvert func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error)

// Interceptor provide a unary interceptor for gRPC.
type Interceptor struct {
	errConvert  ErrConvert
	spanOptions []span.Option
	intercepts  []grpc.UnaryServerInterceptor
	chain       grpc.UnaryServerInterceptor
}

// Option is an option for NewUnary.
type Option func(i *Interceptor) error

// WithSpanOptions adds span options to the interceptor.
func WithSpanOptions(spanOptions ...span.Option) Option {
	return func(i *Interceptor) error {
		for _, opt := range spanOptions {
			if opt == nil {
				return errors.New("span option is nil")
			}
		}
		i.spanOptions = spanOptions
		return nil
	}
}

// WithIntercept adds intercepts to the interceptor.
func WithIntercept(intercepts ...grpc.UnaryServerInterceptor) Option {
	return func(i *Interceptor) error {
		for _, inter := range intercepts {
			if inter == nil {
				return errors.New("intercept is nil")
			}
		}
		i.intercepts = intercepts
		return nil
	}
}

// New creates a new unary interceptor for gRPC. errConvert can be nil.
func New(ctx context.Context, errConvert ErrConvert, options ...Option) (*Interceptor, error) {
	u := &Interceptor{errConvert: errConvert}

	for _, o := range options {
		if err := o(u); err != nil {
			return nil, err
		}
	}

	intercepts := make([]grpc.UnaryServerInterceptor, 0, len(u.intercepts)+2)
	intercepts = append(intercepts, u.attachMeta, u.errLogAndConvert)
	intercepts = append(intercepts, u.intercepts...)
	u.chain = middle.ChainUnaryServer(intercepts...)

	return u, nil
}

// Intercept is the main interceptor function that will be called by gRPC.
// It adds a trace span and then calls the chain of interceptors.
// The span options are applied to the span created for this call.
func (u *Interceptor) Intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	// Add our trace span.
	opts := make([]span.Option, 0, len(u.spanOptions)+1)
	opts = append(opts, span.WithName(info.FullMethod))
	opts = append(opts, u.spanOptions...)
	ctx, spanner := span.New(ctx, opts...)
	defer spanner.End()

	return u.chain(ctx, req, info, handler)
}

// attachMeta in an interceptor that attaches metadata to the context for the gRPC call.
func (u *Interceptor) attachMeta(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	grpcMeta := grpcContext.Metadata{CallID: mustUUID().String(), Op: info.FullMethod}
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		id := md.Get("customerID")
		if len(id) == 1 {
			grpcMeta.CustomerID = id[0]
		}
	}
	ctx = context.Attach(grpcContext.SetMetadata(ctx, grpcMeta))

	return handler(ctx, req)
}

// errLogAndConvert is an interceptor that logs the error and converts it to a gRPC status.
func (u *Interceptor) errLogAndConvert(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	// func(ctx context.Context, req any) (any, error
	resp, err := handler(ctx, req)
	if err == nil {
		return resp, nil
	}

	md := grpcContext.GetMetadata(ctx)

	if e, ok := err.(errors.Error); ok {
		e.Log(ctx, md.CallID, md.CustomerID, req)
		if u.errConvert != nil {
			status, cErr := u.errConvert(ctx, e, md)
			if cErr != nil {
				ce := errors.E(ctx, nil, nil, cErr)
				ce.Log(ctx, md.CallID, md.CustomerID, req)
				return resp, e
			}
			return resp, status.Err()
		}
		return resp, e
	}
	e := errors.E(ctx, nil, nil, err)
	e.Log(ctx, md.CallID, md.CustomerID, req)
	return resp, e
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
