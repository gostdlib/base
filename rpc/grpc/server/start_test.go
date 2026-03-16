package grpc

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
)

func TestRouterTLSRejectionReturns(t *testing.T) {
	t.Parallel()

	// Create a starter with TLS certs configured and an httpHandler that would
	// panic if reached after TLS rejection.
	s := &starter{
		server: &Server{},
		opts: startOptions{
			certs: []tls.Certificate{{}},
			httpHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("TestRouterTLSRejectionReturns: httpHandler was called after TLS rejection, want early return")
			}),
		},
	}

	handler := s.router()

	// Send a non-TLS request (r.TLS == nil) to a server that requires TLS.
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUpgradeRequired {
		t.Errorf("TestRouterTLSRejectionReturns: got status %d, want %d", rec.Code, http.StatusUpgradeRequired)
	}
}

func TestListenGRPCOnlyNilsServerAfterStop(t *testing.T) {
	t.Parallel()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("TestListenGRPCOnlyNilsServerAfterStop: failed to listen: %s", err)
	}

	srv := &Server{
		server: grpc.NewServer(),
	}

	st := &starter{
		server: srv,
		lis:    lis,
	}

	_, err = st.listenGRPCOnly(t.Context())
	if err != nil {
		t.Fatalf("TestListenGRPCOnlyNilsServerAfterStop: got err == %s, want err == nil", err)
	}

	// Stop the gRPC server so the goroutine completes.
	srv.server.GracefulStop()

	// Wait for the done channel to close, indicating the goroutine has finished.
	select {
	case <-srv.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("TestListenGRPCOnlyNilsServerAfterStop: timed out waiting for done channel")
	}

	// After the goroutine completes, server.server should be nil.
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.server != nil {
		t.Errorf("TestListenGRPCOnlyNilsServerAfterStop: got server.server != nil, want nil after shutdown")
	}
}
