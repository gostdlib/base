// Package stream
package stream

import (
	"github.com/google/uuid"
	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/errors"

	grpcContext "github.com/gostdlib/base/context/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Intercept is a user provided interceptor for the stream.
// If it returns an error, the call is aborted and the error is returned to the client.
// and the error is returned to the client. It should be noted that interceptors on Send
// are called before the message is sent and interceptors on Recv are called after the
// message is received.
type Intercept interface {
	// Send is called before a message is sent. The grpc.ServerStream has methods that can only be used
	// at certain times. Like SetHeader() which can only be used before the first message is sent, which
	// happens on the return of Intercept.Send() if there is no error.
	Send(ctx context.Context, md grpcContext.Metadata, ss grpc.ServerStream, m any) error
	// Recv is called after a message is received but before it is passed to the handler.
	Recv(ctx context.Context, md grpcContext.Metadata, m any) error
}

// ErrConvert is a function that converts an error to a gRPC status.
type ErrConvert func(ctx context.Context, e errors.Error, meta grpcContext.Metadata) (*status.Status, error)

// Interceptor provides a grpc.StreamServerInterceptor that wraps the provided interceptors.
// It is used to add interceptors to the grpc server. Use the .Intercept() method as the
// grpc.StreamServerInterceptor.
type Interceptor struct {
	errConvert ErrConvert
	intercepts []Intercept
}

// Options is an option for New.
type Options func(*Interceptor) error

// WithInterceptor adds a stream server interceptors to be used.
func WithIntercepts(i ...Intercept) Options {
	return func(interceptor *Interceptor) error {
		interceptor.intercepts = i
		return nil
	}
}

// New creates a new stream server interceptor that wraps the provided interceptors.
// Our interceptor is always first in the chain.
func New(ctx context.Context, errConvert ErrConvert, options ...Options) (*Interceptor, error) {
	i := &Interceptor{}
	for _, o := range options {
		if err := o(i); err != nil {
			return nil, err
		}
	}

	return i, nil
}

// Intercept is a grpc.StreamServerInterceptor that wraps the provided interceptors.
func (s *Interceptor) Intercept(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	grpcMeta := grpcContext.Metadata{CallID: mustUUID().String(), Op: info.FullMethod}
	md, ok := metadata.FromIncomingContext(ss.Context())
	if ok {
		id := md.Get("customerID")
		if len(id) == 1 {
			grpcMeta.CustomerID = id[0]
		}
	}

	wrapped := &streamWrap{
		ctx:          context.Attach(ss.Context()),
		md:           grpcMeta,
		intercepts:   s.intercepts,
		ServerStream: ss,
	}
	// Call the handler with the wrapped stream
	return handler(srv, wrapped)
}

// streamWrap wraps grpc.ServerStream to add interceptors for sending and receiving messages.]
// This acts as our top level interceptor. It is called by the grpc server when a new stream is created.
type streamWrap struct {
	ctx        context.Context
	md         grpcContext.Metadata
	intercepts []Intercept
	errConvert ErrConvert

	grpc.ServerStream
}

// SendMsg is a wrapper around the SendMsg method of the grpc.ServerStream. It calls all the
// intercepts in the order they were added. If any of the intercepts return an error, it
// aborts the call and returns the error.
func (s *streamWrap) SendMsg(m interface{}) error {
	for _, i := range s.intercepts {
		if err := i.Send(s.ctx, s.md, s.ServerStream, m); err != nil {
			return s.errLogAndConvert(s.ctx, nil, err, s.md)
		}
	}

	if err := s.ServerStream.SendMsg(m); err != nil {
		return s.errLogAndConvert(s.ctx, nil, err, s.md)
	}
	return nil
}

// RecvMsg is a wrapper around the RecvMsg method of the grpc.ServerStream. Unlike Send(), RecvMsg
// will call RecvMsg before it calls the intercepts. If any of the intercepts return an error, it
// aborts the call and returns the error.
func (s *streamWrap) RecvMsg(m interface{}) error {
	err := s.ServerStream.RecvMsg(m)
	if err != nil {
		return s.errLogAndConvert(s.ctx, nil, err, s.md)
	}

	for _, i := range s.intercepts {
		if err := i.Recv(s.ctx, s.md, m); err != nil {
			return s.errLogAndConvert(s.ctx, nil, err, s.md)
		}
	}
	return nil
}

func (s *streamWrap) errLogAndConvert(ctx context.Context, req any, err error, md grpcContext.Metadata) error {
	if e, ok := err.(errors.Error); ok {
		e.Log(ctx, md.CallID, md.CustomerID, req)
		if s.errConvert != nil {
			status, err := s.errConvert(ctx, e, md)
			if err != nil {
				return err
			}
			return status.Err()
		}
		return e
	}
	e := errors.E(ctx, nil, nil, err)
	e.Log(ctx, md.CallID, md.CustomerID, req)
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
