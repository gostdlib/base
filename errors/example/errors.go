/*
Package errors provides the standard library's errors package with additional functionality.
*/
package errors

import (
	"context"
	"regexp"
	"unsafe"

	"github.com/gostdlib/base/errors"

	"google.golang.org/grpc/codes"

	pb "github.com/gostdlib/base/errors/example/proto"
)

//go:generate stringer -type=Category -linecomment

// Category represents the category of the error.
type Category uint32

func (c Category) Category() string {
	return c.String()
}

// Code returns the gRPC status code for the category.
func (c Category) Code() codes.Code {
	return catToCode[c]
}

const (
	// CatUnknown represents an unknown category. This should not be used.
	CatUnknown Category = Category(pb.ErrorCategory_EC_UNKNOWN) // Unknown
	// CatRequest represents an error that is caused by the request being incorrect.
	CatRequest Category = Category(pb.ErrorCategory_EC_REQUEST) // Request
	// CatSafety represents an error that is caused by a safety issue.
	CatSafety Category = Category(pb.ErrorCategory_EC_SAFETY) // Safety
	// CatPermission represents an error that is caused by a permission issue.
	CatPermission Category = Category(pb.ErrorCategory_EC_PERMISSION) // Permission
	// CatResourceExhausted represents an error that is caused by a resource being exhausted.
	CatResourceExhausted Category = Category(pb.ErrorCategory_EC_RESOURCE_EXHAUSTED) // ResourceExhausted
	// CatInternal represents an error that is caused by an internal issue.
	CatInternal Category = Category(pb.ErrorCategory_EC_INTERNAL) // Internal
)

// catToCode maps a Category to a gRPC status code.
var catToCode = map[Category]codes.Code{
	CatUnknown:           codes.Unknown,
	CatRequest:           codes.InvalidArgument,
	CatSafety:            codes.FailedPrecondition,
	CatPermission:        codes.PermissionDenied,
	CatResourceExhausted: codes.ResourceExhausted,
	CatInternal:          codes.Internal,
}

func init() {
	if len(catToCode) != int(CatInternal)+1 {
		panic("catToCode is not complete")
	}
}

var catToProto = map[Category]pb.ErrorCategory{
	CatUnknown:           pb.ErrorCategory_EC_UNKNOWN,
	CatRequest:           pb.ErrorCategory_EC_REQUEST,
	CatSafety:            pb.ErrorCategory_EC_SAFETY,
	CatPermission:        pb.ErrorCategory_EC_PERMISSION,
	CatResourceExhausted: pb.ErrorCategory_EC_RESOURCE_EXHAUSTED,
	CatInternal:          pb.ErrorCategory_EC_INTERNAL,
}

func init() {
	if len(catToProto) != len(pb.ErrorType_name) {
		panic("catToProto is not complete")
	}
}

//go:generate stringer -type=Type -linecomment

// Type represents the type of the error.
type Type uint16

func (t Type) Type() string {
	return t.String()
}

const (
	// TypeUnknown represents an unknown type.
	TypeUnknown Type = Type(pb.ErrorType_ET_UNKNOWN) // Unknown

	// 1-100 are RPC errors.

	// TypeBadRequest represents a bad request error.
	TypeBadRequest Type = Type(pb.ErrorType_ET_BAD_REQUEST) // BadRequest

	// 101-200 are safety errors.

	// TypeDeadTimer represents that a resource is restricted by a dead timer. Until that
	// timer expires, the resource cannot be accessed for this use.
	TypeDeadTimer Type = Type(pb.ErrorType_ET_DEAD_TIMER) // DeadTimer
	// TypeClusterHealth represents that the cluster is unhealthy and cannot be accessed.
	TypeClusterHealth Type = Type(pb.ErrorType_ET_CLUSTER_HEALTH) // ClusterHealth

	// 201-300 are permission errors.

	// TypePermissionDenied represents that the user does not have permission to access the resource.
	TypePermissionDenied Type = Type(pb.ErrorType_ET_PERMISSION_DENIED) // PermissionDenied
)

var typeToProto = map[Type]pb.ErrorType{
	TypeUnknown:          pb.ErrorType_ET_UNKNOWN,
	TypeBadRequest:       pb.ErrorType_ET_BAD_REQUEST,
	TypeDeadTimer:        pb.ErrorType_ET_DEAD_TIMER,
	TypeClusterHealth:    pb.ErrorType_ET_CLUSTER_HEALTH,
	TypePermissionDenied: pb.ErrorType_ET_PERMISSION_DENIED,
}

func init() {
	if len(typeToProto) != len(pb.ErrorType_name) {
		panic("typeToProto is not complete")
	}
}

// Error is our base error type.
type Error = errors.Error

type newOptions struct {
	disableSecretDetection bool
}

// Option is a function that modifies the options.
type Option func(newOptions) newOptions

// WithNoSecretDetection returns an Option that will prevent secret detection from scrubbing the error message.
func WithNoSecretDetection() Option {
	return func(o newOptions) newOptions {
		o.disableSecretDetection = true
		return o
	}
}

var secretRE = regexp.MustCompile(`(?i)(token|pass|jwt|hash|secret|bearer|cred|secure|signing|cert|code|key)`)

// E creates a new Error with the given category, type, message, and arguments. It will
// automatically redact any secrets from the error message unless WithNoSecretDetection is passed.
func E(ctx context.Context, c Category, t Type, msg error, options ...Option) Error {
	opts := newOptions{}

	for _, o := range options {
		opts = o(opts)
	}

	if opts.disableSecretDetection {
		return errors.E(ctx, c, t, msg)
	}
	if secretRE.MatchString(msg.Error()) {
		e := errors.E(ctx, c, t, msg)
		e.MsgOverride = "[redacted for security]"
		return e
	}
	return errors.E(ctx, c, t, msg)
}

// bytesToStr converts a byte slice to a string without copying the data.
func bytesToStr(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(&b[0], len(b))
}
