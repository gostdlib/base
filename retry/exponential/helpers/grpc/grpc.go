/*
Package gRPC provides an exponential.ErrTransformer that can be used to detect non-retriable errors for gRPC calls.
There is no direct support for gRPC streaming in this package.

Example using just defaults:

	// This will retry any grpc error codes that are considered retriable.
	grpcErrTransform, _ := grpc.New() // Uses defaults

	backoff := exponential.WithErrTransformer(grpcErrTransform)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req := &pb.HelloRequest{Name: "John"}
	var resp *pb.HelloReply{}

	err := backoff.Retry(
		ctx,
		func(ctx context.Context, r Record) error {
			var err error
			resp, err = client.SayHello(ctx, req)
			return err
		},
	)
	cancel()

Example setting an extra code for retries:

	// The same as above, except we will retry on codes.DataLoss.
	grpcErrTransform, err := grpc.New(WithExtraCodes(codes.DataLoss))
	if err != nil {
		// Handle error
	}
	... // The rest is the same

Example with custom message inspection:

	// We are going to provide a function that can inspect a proto.Message when
	// the client did not send an error, but there was an error sent back from the server
	// in the response.
	respHasErr := func (msg proto.Message) error {
		r := msg.(*pb.HelloReply)

		if r.Error != "" {
			if r.PermanentErr {
				// This will stop retries.
				return fmt.Errorf("%s: %w", r.Error, errors.ErrPermanent)
			}
			// We can still retry.
			return fmt.Errorf("%s", r.Error)
		}
		return nil
	}
	grpcErrTransform, err := grpc.New(WithProtoToErr(respHasErr))
	if err != nil {
		// Handle error
	}

	backoff := exponential.WithErrTransformer(grpcErrTransform)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req := &pb.HelloRequest{Name: "John"}
	var resp *pb.HelloReply{}

	err := backoff.Retry(
		ctx,
		func(ctx context.Context, r Record) error {
			a, err := grpcErrTransform.RespToErr(client.SayHello(ctx, req)) // <- Notice the call wrapper
			if err != nil {
				return err
			}
			resp = a.(*pb.HelloReply)
			return nil
		},
	)
	cancel()
*/
package grpc

import (
	"github.com/Azure/retry/exponential/helpers/grpc"
)

/*
Transformer provides an ErrTransformer method that can be used to detect non-retriable errors.
The following codes are retriable: Canceled, DeadlineExceeded, Unknown, Internal, Unavailable, ResourceExhausted.
Any other code is not.
*/
type Transformer = grpc.Transformer

// Option is an option for the New() constructor.
type Option = grpc.Option

// ProtoToErr inspects a protocol buffer message and determines if the call was really an error.
// If it was not, this returns nil.
type ProtoToErr = grpc.ProtoToErr

// WithProtoToErrs pass functions that look at protocol buffer message responses to determine if
// the message actually indicates an error.
func WithProtoToErrs(protosToErrs ...ProtoToErr) Option {
	return grpc.WithProtoToErrs(protosToErrs...)
}

// New returns a new Transformer. This implements exponential.ErrTransformer with the method ErrTransformer.
// You can add other codes that are retriable by passing them as arguments. This list of retriable codes
// are listed on Transformer.
func New(options ...Option) (*Transformer, error) {
	return grpc.New(options...)
}
