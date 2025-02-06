// Package grpc provides getters and setters for gRPC data on our context object.
package grpc

import (
	"context"
	"unique"
)

type metadataKey struct{}

var zeroMetadata = unique.Make(Metadata{})

// Metadata is a struct that contains metadata about an RPC call.
type Metadata struct {
	// CustomerID is the customer ID for the call. This is set by the customer.
	CustomerID string
	// CallID is a unique identifier for the call. This is set by an interceptor.
	CallID string
	// Op represents the operation being performed. This is the method being
	// called in gRPC. This is set by an interceptor.
	Op string
}

// IsZero returns true if the RPCMetadata is the zero value.
func (r Metadata) IsZero() bool {
	return unique.Make(r) == zeroMetadata
}

// SetRPCMetadata attaches RPCMetadata to the context. This will normally be set by the middleware
// and correspond to a specific operation in grpc or http. This can be used to do things like cause
// traces to occur for an operation.
func SetMetadata(ctx context.Context, md Metadata) context.Context {
	return context.WithValue(ctx, metadataKey{}, md)
}

// GetMetadata returns the RPCMetadata attached to the context. If no RPCMetadata is attached,
// it returns the zero value.
func GetMetadata(ctx context.Context) Metadata {
	if ctx == nil {
		return Metadata{}
	}
	a := ctx.Value(metadataKey{})
	if a == nil {
		return Metadata{}
	}
	v, ok := a.(Metadata)
	if !ok {
		return Metadata{}
	}
	return v
}
