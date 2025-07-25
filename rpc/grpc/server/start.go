package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	baseContext "github.com/gostdlib/base/context"
	"github.com/gostdlib/base/rpc/grpc/server/internal/interceptors/stream"
	"github.com/gostdlib/base/rpc/grpc/server/internal/interceptors/unary"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	"github.com/CAFxX/httpcompression"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

func defaultHTTP() http.Server {
	return http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       1 * time.Minute,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          log.New(io.Discard, "", 0),
		BaseContext: func(net.Listener) context.Context {
			return baseContext.Background()
		},
	}
}

// fn is a function that returns the next state of the statemachine or an error.
// If the next state is nil, the statemachine will stop.
// If the error is not nil, the statemachine will stop and return the error.
type fn func(ctx context.Context) (fn, error)

// starter is a statemachine for starting a server. This allows for easier
// testing of the server startup process by dividing it into states that
// can be tested individually. It also allows for easier extension of the
// startup process by adding new states.
type starter struct {
	server *Server

	lis                net.Listener
	options            []StartOption
	unaryInterceptors  []unary.Intercept
	streamInterceptors []stream.Intercept

	opts startOptions
}

// Run runs the statemachine.
func (s *starter) Run(ctx context.Context) error {
	s.server.mu.Lock()
	defer s.server.mu.Unlock()

	var err error
	for _, o := range s.options {
		s.opts, err = o(s.opts)
		if err != nil {
			return err
		}
	}

	for f := s.start; f != nil; {
		f, err = f(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

// start is the first state of the statemachine.
func (s *starter) start(ctx context.Context) (fn, error) {
	if s.server.server != nil {
		return nil, fmt.Errorf("server already started")
	}

	if len(s.server.registrations) == 0 {
		return nil, fmt.Errorf("no services registered")
	}

	return s.setOptions(ctx)
}

// setOptions sets our default server options and then calls the options
// provided by the user.
func (s *starter) setOptions(ctx context.Context) (fn, error) {
	fmt.Printf("[DEBUG] s.unaryInterceptors: %T\n", s.unaryInterceptors)
	ui, err := unary.New(ctx, unary.ErrConvert(s.opts.errConverter), unary.WithIntercept(s.unaryInterceptors...))
	if err != nil {
		return nil, err
	}

	si, err := stream.New(ctx, stream.ErrConvert(s.opts.errConverter), stream.WithIntercepts(s.streamInterceptors...))
	if err != nil {
		return nil, err
	}

	// Setup a base for the options that gets modified.
	s.opts = startOptions{
		serverOptions: []grpc.ServerOption{
			grpc.UnaryInterceptor(ui.Intercept),
			grpc.StreamInterceptor(si.Intercept),
			// grpc.StatsHandler(otelgrpc.NewServerHandler()),
			grpc.KeepaliveParams(defaultKeepalive),
			grpc.MaxConcurrentStreams(100),          // Limit concurrent streams
			grpc.ConnectionTimeout(5 * time.Second), // Timeout for new connections
		},
		gwDial: []grpc.DialOption{grpc.WithBlock()},
	}

	for _, o := range s.options {
		s.opts, err = o(s.opts)
		if err != nil {
			return nil, err
		}
	}
	if len(s.opts.certs) == 0 {
		s.opts.serverOptions = append(s.opts.serverOptions, grpc.Creds(insecure.NewCredentials()))
	}

	return s.registrations, nil
}

var (
	errMissingMetadata = status.Errorf(codes.InvalidArgument, "missing metadata")
	errInvalidToken    = status.Errorf(codes.Unauthenticated, "invalid token")
)

func unaryInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	// authentication (token verification)
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, errMissingMetadata
	}
	if val, ok := md["authorization"]; !ok || val == nil || len(val) == 0 {
		return nil, errInvalidToken
	}
	m, err := handler(ctx, req)
	if err != nil {
		fmt.Printf("[DEBUG] unary interceptor failed")
		// logger("RPC failed with error: %v", err)
	}
	return m, err
}

// registrations registers the services with the server.
func (s *starter) registrations(ctx context.Context) (fn, error) {
	s.server.server = grpc.NewServer(s.opts.serverOptions...)

	for _, reg := range s.server.registrations {
		s.server.server.RegisterService(reg.desc, reg.impl)
	}
	return s.health, nil
}

// health registers the health service with the server.
func (s *starter) health(ctx context.Context) (fn, error) {
	s.server.health = s.opts.health
	if s.server.health == nil {
		s.server.health = health.NewServer()
	}
	grpc_health_v1.RegisterHealthServer(s.server.server, s.server.health)
	return s.reflection, nil
}

// reflection registers the reflection service with the server.
func (s *starter) reflection(ctx context.Context) (fn, error) {
	reflection.Register(s.server.server)

	return s.setupGW, nil
}

func (s *starter) setupGW(ctx context.Context) (fn, error) {
	if s.opts.gwReg == nil {
		return s.setupHTTP, nil
	}
	var rmux = s.opts.mux
	if rmux == nil {
		rmux = runtime.NewServeMux()
		s.opts.mux = rmux
	}

	if len(s.opts.certs) > 0 {
		tlsconf := &tls.Config{InsecureSkipVerify: true} // We are dialing ourselves, so it should be fine.
		creds := credentials.NewTLS(tlsconf)
		s.opts.gwDial = append(s.opts.gwDial, grpc.WithTransportCredentials(creds))
	}

	if err := s.opts.gwReg(ctx, rmux, s.lis.Addr().String(), s.opts.gwDial); err != nil {
		return nil, err
	}

	return s.setupHTTP, nil
}

var compress, _ = httpcompression.DefaultAdapter()

func (s *starter) setupHTTP(ctx context.Context) (fn, error) {
	if s.opts.httpHandler == nil {
		return s.listen, nil
	}

	s.opts.httpHandler = compress(s.opts.httpHandler)
	return s.listen, nil
}

// listen starts the server listening on the provided listener.
func (s *starter) listen(ctx context.Context) (fn, error) {
	if s.opts.gwReg == nil && s.opts.httpHandler == nil {
		return s.listenGRPCOnly, nil
	}
	return s.listenWithHTTP, nil
}

func (s *starter) listenGRPCOnly(ctx context.Context) (fn, error) {
	s.server.done = make(chan error, 1)

	go func() {
		defer close(s.server.done)

		s.server.done <- s.server.server.Serve(s.lis)

		s.server.mu.Lock()
		defer s.server.mu.Unlock()
		s.server = nil
	}()
	return nil, nil
}

func (s *starter) listenWithHTTP(ctx context.Context) (fn, error) {
	s.server.done = make(chan error)

	mux := s.opts.mux
	if mux == nil {
		mux = runtime.NewServeMux()
		s.opts.mux = mux
	}

	if len(s.opts.certs) == 0 {
		return nil, s.startNonTLS(ctx)
	}
	return nil, s.startTLS(ctx)
}

func (s *starter) startTLS(ctx context.Context) error {
	h := s.buildHTTP(ctx)
	/*
		h.TLSConfig = &tls.Config{
			Certificates: s.opts.certs,
			NextProtos:   []string{"h2", "http/1.1"},
		}
	*/
	//h.TLSConfig.BuildNameToCertificate() // says it is deprecated, but if it doesn't work try it.

	s.server.done = make(chan error, 1)
	go func() {
		defer close(s.server.done)

		s.server.done <- h.ServeTLS(s.lis, "", "")

		s.server.mu.Lock()
		defer s.server.mu.Unlock()
		s.server = nil
	}()

	return nil
}

func (s *starter) startNonTLS(ctx context.Context) error {
	h := s.buildHTTP(ctx)

	go func() {
		defer close(s.server.done)

		s.server.done <- h.Serve(s.lis)

		s.server.mu.Lock()
		defer s.server.mu.Unlock()
		s.server = nil
	}()

	return nil
}

// buildHTTP builds the http server supporting gRPC, a gateway if specified, and a handler for other http.
// If no certs were provided, the server will use h2c.
func (s *starter) buildHTTP(ctx context.Context) *http.Server {
	hServer := s.opts.httpServer
	if hServer == nil {
		h := defaultHTTP()
		hServer = &h
	}

	if len(s.opts.certs) == 0 {
		hServer.Handler = h2c.NewHandler(s.router(), &http2.Server{})
		return hServer
	}
	hServer.TLSConfig = &tls.Config{
		Certificates: s.opts.certs,
		NextProtos:   []string{"h2", "http/1.1"},
	}
	hServer.Addr = ""
	hServer.Handler = s.router()
	return hServer
}

// router routes the incoming requests to the appropriate handler.
func (s *starter) router() http.Handler {
	if s.opts.mux == nil {
		s.opts.mux = runtime.NewServeMux()
	}

	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if len(s.opts.certs) > 0 && r.TLS == nil {
				http.Error(w, "TLS required", http.StatusUpgradeRequired)
			}
			if r.ProtoMajor == 2 {
				switch r.Header.Get("Content-Type") {
				case "application/grpc":
					s.server.server.ServeHTTP(w, r)
				case "application/grpc-gateway", "application/jsonpb":
					s.opts.mux.ServeHTTP(w, r)
				default:
					s.opts.httpHandler.ServeHTTP(w, r)
				}
			} else {
				switch r.Header.Get("Content-Type") {
				case "application/grpc-gateway", "application/jsonpb":
					s.opts.mux.ServeHTTP(w, r)
				default:
					s.opts.httpHandler.ServeHTTP(w, r)
				}
			}
		},
	)
}
