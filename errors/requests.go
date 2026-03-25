package errors

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"

	"github.com/go-json-experiment/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// RequestConversion is a function type that defines how to convert a request object of type T into a string representation.
// This is intended to be used for logging purposes. If the conversion fails, it should return a string that indicates the
// failure, rather than an error, as this will be attached to log attributes.
type RequestConversion func(ctx context.Context, v any) string

// RequestSwitchCase is a struct that holds a reflect.Type and a corresponding RequestConversion function.
// This allows you to define different conversion strategies for different types of request objects, and switch between them at
// runtime based on the type of the request.
type RequestSwitchCase struct {
	Type       reflect.Type
	Conversion RequestConversion
}

// RequestToStr provides a methodology for converting a request object to a string. This allows you to apply it
// as an attribute value for use in logging.
type RequestToStr struct {
	Switch  []RequestSwitchCase
	Default RequestConversion
}

// Convert takes a request object and attempts to convert it to a string using the defined RequestSwitches.
// If no matching type is found, it uses the Default conversion function if provided, or returns a message
// indicating that no conversion was found.
func (r *RequestToStr) Convert(ctx context.Context, req any) string {
	rt := reflect.TypeOf(req)
	for _, s := range r.Switch {
		if rt == s.Type || (s.Type.Kind() == reflect.Interface && rt != nil && rt.Implements(s.Type)) {
			return s.Conversion(ctx, req)
		}
	}
	if r.Default == nil {
		return fmt.Sprintf("no conversion found for request of type %T", req)
	}
	return r.Default(ctx, req)
}

// ProtoRequestToStr is a RequestConversion function that converts a proto.Message into its JSON string representation.
func ProtoRequestToStr(ctx context.Context, v any) string {
	var err error
	msg, ok := v.(proto.Message)
	if !ok {
		return fmt.Sprintf("ProtoRequestToStr received a non-proto.Message type: %T", v)
	}

	reqBytes, err := protojson.Marshal(msg)
	if err != nil {
		reqBytes = fmt.Appendf(reqBytes, "unable to marshal proto.Message due to error: %s", err)
	}
	return bytesToStr(reqBytes)
}

// HTTPRequestToStr is a RequestConversion function that converts v into its JSON string representation. v must be *http.Request.
func HTTPRequestToStr(ctx context.Context, v any) string {
	var err error
	msg, ok := v.(*http.Request)
	if !ok {
		return fmt.Sprintf("HTTPRequestToStr received a non-*http.Request type: %T", v)
	}
	reqBytes, err := requestToJSON(msg)
	if err != nil {
		reqBytes = fmt.Appendf(reqBytes, "unable to marshal *http.Request due to error: %s", err)
	}
	return bytesToStr(reqBytes)
}

// ObjectToStr is a RequestConversion function that attempts to convert any object into a string representation.
// It attempts a JSON conversion.
func ObjectToStr(ctx context.Context, v any) string {
	reqBytes, err := json.Marshal(v)
	if err != nil {
		reqBytes = fmt.Appendf(reqBytes, "unable to marshal request %T object due to error: %s", v, err.Error())
	}
	return bytesToStr(reqBytes)
}

// JSONRequest is a simplified version of http.Request for JSON serialization
type JSONRequest struct {
	Method        string              `json:"method"`
	URL           string              `json:"url"`
	Header        map[string][]string `json:"header"`
	ContentLength int64               `json:"content_length"`
	Host          string              `json:"host"`
	RemoteAddr    string              `json:"remote_addr"`
	RequestURI    string              `json:"request_uri"`
	Body          string              `json:"body"`
}

func requestToJSON(r *http.Request) ([]byte, error) {
	var bodyBytes []byte
	// Read the original body
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		r.Body.Close()
	}

	// Reset the request body so it can be read again
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	jr := JSONRequest{
		Method:        r.Method,
		URL:           r.URL.String(),
		Header:        r.Header,
		ContentLength: r.ContentLength,
		Host:          r.Host,
		RemoteAddr:    r.RemoteAddr,
		RequestURI:    r.RequestURI,
		Body:          bytesToStr(bodyBytes),
	}
	return json.Marshal(jr)
}
