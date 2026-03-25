// Package converters provides types to convert a request into a string representation.
package converters

import (
	"context"
	"net/http"
	"reflect"

	"github.com/gostdlib/base/errors"
	"google.golang.org/protobuf/proto"
)

var requestToStr = errors.RequestToStr{
	Switch: []errors.RequestSwitchCase{
		{
			Type:       reflect.TypeOf((*proto.Message)(nil)).Elem(),
			Conversion: errors.ProtoRequestToStr,
		},
		{
			Type:       reflect.TypeOf((*http.Request)(nil)),
			Conversion: errors.HTTPRequestToStr,
		},
	},
	Default: errors.ObjectToStr,
}

// Request converts a request object into a string representation using the defined RequestToStr.
func Request(ctx context.Context, req any) string {
	return requestToStr.Convert(ctx, req)
}
