// Package grpc provides a wrapper around the google.golang.org/grpc package that integrates features
// into our gRPC servers. This takes advantage of all the packages in base/ to ease the developer experience.
// Here is a list of some of the features that are integrated:
//   - grpc reflection is enabled by default.
//   - grpc metrics are collected by default.
//   - Tracing is enabled by default and a default span is created at each endpoint ingress.
//   - Error logging is enabled to automatically log an error in the RPC path.
//   - Context values are attached at the ingress point for all RPCs.
//   - Compression is enabled for gzip on gRPC if specified by the client.
//   - Compression support for gzip, brotil, defalte and zstd are supported for http.
//   - Sets up the default health check service and exposes it via Health() to allow for the user to manipulate.
//
// It also defines some sane defaults for gRPC that can be overridden by passing options to WithServerOptions().
// Here is a list:
//   - Keepalive is set to 1 minute idle, 10 second ping interval, and 5 second ping timeout.
//   - Limit of 100 concurrent connections.
//   - Connection timeout of 5 seconds.
//
// It also defines some sane defaults for an HTTP server if not specified by the user. Here is a list:
//   - ReadHeaderTimeout is set to 5 seconds.
//   - IdleTimeout is set to 1 minute.
//   - MaxHeaderBytes is set to 1MB.
//   - ErrorLog is set to a /dev/null logger
//   - BaseContext is set to our base/context.Background()
//
// This package also offers other ammenities such as:
//   - gRPC Gateway integration.
//   - HTTP port sharing with gRPC. This avoids CORS issues if your app uses the Gateway.
//   - Support for H2C (HTTP/2 Cleartext) with port sharing when using non-TLS connections.
package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	grpcContext "github.com/gostdlib/base/context/grpc"

	"github.com/gostdlib/base/errors"
	goinit "github.com/gostdlib/base/init"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	_ "google.golang.org/grpc/encoding/gzip"
)

var defaultKeepalive = keepalive.ServerParameters{
	MaxConnectionIdle: 1 * time.Minute,  // Close idle connections
	Time:              10 * time.Second, // Ping interval
	Timeout:           5 * time.Second,  // Ping timeout
}

// reg is grpc service registration information.
type reg struct {
	desc *grpc.ServiceDesc
	impl any
}

// Server is a gRPC server. It provides automatic integration with logging, tracing and metrics for gRPC servers.
// It also integrates the base/context package to attach various values to the context.
type Server struct {
	mu            sync.Mutex
	registrations []reg
	server        *grpc.Server
	health        grpc_health_v1.HealthServer
	done          chan error
}

// Option is an optional argument to New.
type Option func(*Server) error

// New creates a new gRPC server.
func New(options ...Option) (*Server, error) {
	if !goinit.Called() {
		return nil, fmt.Errorf("grpc.New() must be called after goinit.Service()")
	}

	s := &Server{}
	for _, option := range options {
		if err := option(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

type startOptions struct {
	serverOptions []grpc.ServerOption
	gwReg         GWRegistration
	gwDial        []grpc.DialOption
	httpHandler   http.Handler
	httpServer    *http.Server
	health        grpc_health_v1.HealthServer
	certs         []tls.Certificate
	errConverter  ErrConverter

	mux *runtime.ServeMux
}

// StartOption is an optional argument to the Start method.
type StartOption func(startOptions) (startOptions, error)

// WithServerOptions appends the gRPC server options for the server. You do not need to pass
// insecure creds. If this server is not using TLS, it will automatically use WithInsecure().
// To run with TLS, use WithTLS().
func WithServerOptions(options ...grpc.ServerOption) StartOption {
	return func(s startOptions) (startOptions, error) {
		s.serverOptions = append(s.serverOptions, options...)
		return s, nil
	}
}

// WithHTTP sets the http.Handler to be used when starting the server. This allows you to have
// other HTTP services on the same port as the gRPC server. You cab build a Handler out of http.ServerMux
// with whatever http wrappers you want to use. All non-gateway traffic and grpc traffic will be
// passed to the handler.
func WithHTTP(h http.Handler) StartOption {
	return func(s startOptions) (startOptions, error) {
		s.httpHandler = h
		return s, nil
	}
}

// WithHTTPServer sets the http.Server to be used as the server. If using this, the timeouts should
// equal or exceed the grpc timeouts. If not using this, a default http.Server will be used which has
// the defaults listed in the godoc for this package.
func WithHTTPServer(h *http.Server) StartOption {
	return func(s startOptions) (startOptions, error) {
		s.httpServer = h
		return s, nil
	}
}

// GWRegistration is a function in your <service>.pb.gw.go file that is used to register a
// GRPC REST gateway to talk to the GRPC service. It is usually named Register<service>HandlerFromEndpoint().
type GWRegistration func(ctx context.Context, mux *runtime.ServeMux, endpoint string, opts []grpc.DialOption) error

// WithGateway enables the gRPC gateway. This allows REST calls to be made to the gRPC server.
func WithGateway(reg GWRegistration, dialOptions ...grpc.DialOption) StartOption {
	return func(s startOptions) (startOptions, error) {
		s.gwReg = reg
		s.gwDial = dialOptions
		return s, nil
	}
}

// WithHealth provides a custom health check service. This allows you to customize the health check service
// that is provided by default. This is useful if you want to add custom health checks to the service.
// Otherwise you can use the default health service to manipulate the health of the server.
// You can get at it via .Health().
func WithHealth(h grpc_health_v1.HealthServer) StartOption {
	return func(s startOptions) (startOptions, error) {
		s.health = h
		return s, nil
	}
}

// WithTLS sets the TLS certificates for the server. This is required if you want to use TLS with the server.
// If not set, the grpc server will run in insecure mode.
func WithTLS(certs []tls.Certificate) StartOption {
	return func(s startOptions) (startOptions, error) {
		s.certs = certs
		return s, nil
	}
}

// GRPCErrConverter is a function that converts an error to a gRPC status. This is useful if you want to
// convert the custom errors in your application to gRPC status codes (which can contain errors). This is called
// at the egress point of the gRPC server if there is an error.
type ErrConverter func(ctx context.Context, err errors.Error, md grpcContext.Metadata) (*status.Status, error)

// WithErrConverter sets an error converter for the server. This allows you to convert errors in your application
// to gRPC status codes with whatever message you want. If not used, this sends back the error as is.
// This is called at the egress point of the gRPC server.
func WithErrConverter(fn ErrConverter) StartOption {
	return func(s startOptions) (startOptions, error) {
		s.errConverter = fn
		return s, nil
	}
}

// RegisterService registers a grpc service with the server. This is just using the underlying methodology
// that happens when you use the server specific Restister* method. desc is the *_ServiceDesc object generated
// by the package. impl is your implementation of the service. The actually registration will not be done until
// the server is started via Start(). Note that you can call this multiple times for different services you want
// to register.
func (s *Server) RegisterService(desc *grpc.ServiceDesc, impl any) {
	s.registrations = append(
		s.registrations,
		reg{desc: desc, impl: impl},
	)
}

// Start starts the gRPC server. This does not block. If you need it to block, call Wait() afterward.
// This returns an error in most cases, however if you called RegisterService with something incorrect,
// this will cause gRPC to panic.
func (s *Server) Start(ctx context.Context, lis net.Listener, options ...StartOption) error {
	start := starter{server: s, lis: lis, options: options}
	return start.Run(ctx)
}

// Wait blocks until the server is stopped or the context is done.
// It returns the error that caused the server to stop or the context error.
func (s *Server) Wait(ctx context.Context) error {
	done := s.getDone()
	if done == nil {
		return fmt.Errorf("server not started")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// Stop stops the gRPC server. If forced is true, it will stop the server immediately, otherwise
// it will wait for all connections to close before stopping. This returns an error if the server
// is not started.
func (s *Server) Stop(ctx context.Context, forced bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server == nil {
		return fmt.Errorf("server not started")
	}

	defer func() {
		s.server = nil
	}()

	if forced {
		s.server.Stop()
		return nil
	}

	s.server.GracefulStop()
	return nil
}

// Health returns the health server for the gRPC server. This allows you to set the status of the server
// and check the status of the server.
func (s *Server) Health() grpc_health_v1.HealthServer {
	return s.health
}

// getDone returns the done channel for the server. This uses a mutex
// to prevent race conditions.
func (s *Server) getDone() <-chan error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}
