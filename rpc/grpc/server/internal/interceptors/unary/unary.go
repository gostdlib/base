package unary

import (
	"github.com/google/uuid"
	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/errors"
	"github.com/gostdlib/base/telemetry/otel/trace/span"

	grpcContext "github.com/gostdlib/base/context/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Intercept is a unary interceptor for gRPC that can be provided by the user. It returns
// the request or an error. This allows modification of the request before it is sent to the handler.
type Intercept func(ctx context.Context, req any, md grpcContext.Metadata) (any, error)

// ErrConvert is a function that converts an error to a gRPC status. If it cannot, it returns a standard error.
type ErrConvert func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error)

// Interceptor provide a unary interceptor for gRPC.
type Interceptor struct {
	errConvert  ErrConvert
	spanOptions []span.Option
	intercepts  []Intercept
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
func WithIntercept(intercepts ...Intercept) Option {
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

// New creates a new unary interceptor for gRPC.
func New(ctx context.Context, errConvert ErrConvert, options ...Option) (*Interceptor, error) {
	u := &Interceptor{errConvert: errConvert}

	for _, o := range options {
		if err := o(u); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// Intercept intercepts unary gRPC calls.
func (u *Interceptor) Intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	grpcMeta := grpcContext.Metadata{CallID: mustUUID().String(), Op: info.FullMethod}
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		id := md.Get("customerID")
		if len(id) == 1 {
			grpcMeta.CustomerID = id[0]
		}
	}
	ctx = context.Attach(grpcContext.SetMetadata(ctx, grpcMeta))

	// Add our trace span.
	opts := make([]span.Option, 0, len(u.spanOptions)+1)
	opts = append(opts, span.WithName(info.FullMethod))
	opts = append(opts, u.spanOptions...)
	ctx, spanner := span.New(ctx, opts...)
	defer spanner.End()

	var err error
	for _, i := range u.intercepts {
		req, err = i(ctx, req, grpcMeta)
		if err != nil {
			return nil, u.errLogAndConvert(ctx, err, grpcMeta, req)
		}
	}

	resp, err := handler(ctx, req)
	if err != nil {
		return nil, u.errLogAndConvert(ctx, err, grpcMeta, req)
	}

	return resp, err
}

func (u *Interceptor) errLogAndConvert(ctx context.Context, err error, grpcMeta grpcContext.Metadata, req any) error {
	if e, ok := err.(errors.Error); ok {
		e.Log(ctx, grpcMeta.CallID, grpcMeta.CustomerID, req)
		if u.errConvert != nil {
			status, cErr := u.errConvert(ctx, e, grpcMeta)
			if cErr != nil {
				ce := errors.E(ctx, nil, nil, cErr)
				ce.Log(ctx, grpcMeta.CallID, grpcMeta.CustomerID, req)
				return e
			}
			return status.Err()
		}
		return e
	}
	e := errors.E(ctx, nil, nil, err)
	e.Log(ctx, grpcMeta.CallID, grpcMeta.CustomerID, req)
	return e
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
