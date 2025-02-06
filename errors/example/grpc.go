package errors

import (
	"context"
	"fmt"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	pb "github.com/gostdlib/base/errors/example/proto"
)

// grpc returns the gRPC *status.Status of the error. The id is the ID of the request that caused the error.
// The req is the request that caused the error. This is used to attach the request to the error message.
func grpc(ctx context.Context, e Error, callID, customerID string, req any) (any, error) {
	reqProto, ok := req.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("grpc error converter requires a proto.Message request, got %T", req)
	}

	var any *anypb.Any

	any, _ = anypb.New(reqProto) // Skip the error because it doesn't matter if it doesn't convert.

	pbErr := &pb.Error{
		Id:       callID,
		Category: pb.ErrorCategory(e.Category.(Category)),
		Type:     pb.ErrorType(e.Type.(Type)),
		Msg:      e.Error(),
		Request:  any,
	}

	b, err := protojson.Marshal(pbErr)
	if err == nil {
		return status.New(catToCode[e.Category.(Category)], bytesToStr(b)), nil
	}

	return status.New(catToCode[e.Category.(Category)], e.Error()), nil
}
