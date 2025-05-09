package etoe

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	grpcContext "github.com/gostdlib/base/context/grpc"
	"github.com/gostdlib/base/errors"
	pb "github.com/gostdlib/base/errors/example/proto"
	goinit "github.com/gostdlib/base/init"
	grpc "github.com/gostdlib/base/rpc/grpc/server"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

var initArgs = goinit.InitArgs{
	Meta: goinit.Meta{
		Service: "base/rpc/grpc/server/testing/etoe",
		Build:   "test",
	},
}

func init() {
	goinit.Service(initArgs)
}

type helloService struct {
	pb.UnimplementedHelloServer
}

func (s *helloService) Hello(ctx context.Context, req *pb.HelloReq) (*pb.HelloResp, error) {
	if req.Name == "" {
		return nil, errors.E(ctx, Category(codes.Internal), nil, fmt.Errorf("name is empty"))
	}
	return &pb.HelloResp{Msg: fmt.Sprintf("Hello, %s!", req.Name)}, nil
}

type restClient struct {
	endpoint string
	client   *http.Client
	useTLS   bool
}

func newREST(endpoint string, useTLS bool) *restClient {
	tlsconf := &tls.Config{InsecureSkipVerify: true}

	client := &http.Client{}
	if useTLS {
		transport := &http.Transport{
			TLSClientConfig: tlsconf,
		}
		client.Transport = transport
	}
	return &restClient{
		endpoint: endpoint,
		client:   client,
		useTLS:   useTLS,
	}
}

func (r *restClient) close() {
	r.client.CloseIdleConnections()
}

func (r *restClient) restGRPCCall(ctx context.Context, req *pb.HelloReq) (*pb.HelloResp, error) {
	if req == nil {
		return nil, fmt.Errorf("req is nil")
	}

	scheme := "http"
	if r.useTLS {
		scheme = "https"
	}

	u := url.URL{
		Scheme: scheme,
		Host:   r.endpoint,
		Path:   "/api/v1/hello",
	}

	b, _ := protojson.Marshal(req)
	hreq := &http.Request{
		Method: "POST",
		URL:    &u,
		Header: http.Header{"Content-Type": []string{"application/grpc-gateway"}},
		Body:   io.NopCloser(bytes.NewReader(b)),
	}

	hresp, err := r.client.Do(hreq)
	if err != nil {
		return nil, err
	}
	defer hresp.Body.Close()

	if hresp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", hresp.StatusCode)
	}

	b, _ = io.ReadAll(hresp.Body)

	var resp pb.HelloResp
	if err := protojson.Unmarshal(b, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Category represents the category of the error.
type Category uint32

// Category returns the error category as a string.
func (c Category) Category() string {
	return codes.Code(c).String()
}

func errConverter(ctx context.Context, err errors.Error, md grpcContext.Metadata) (*status.Status, error) {
	if n, ok := err.Category.(Category); ok {
		return status.New(codes.Code(n), err.Error()), nil
	}
	return status.New(codes.Unknown, err.Error()), nil
}

func TestMostOptions(t *testing.T) {
	cert, err := generateSelfSignedCert()
	if err != nil {
		panic(err)
	}

	tests := []struct {
		name     string
		tlsCerts tls.Certificate
	}{
		{
			name: "insecure",
		},
		{
			name:     "secure",
			tlsCerts: cert,
		},
	}

	for _, test := range tests {
		server, err := grpc.New()
		if err != nil {
			t.Fatal(err)
		}

		lis, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(err)
		}

		server.RegisterService(&pb.Hello_ServiceDesc, &helloService{})

		opts := []grpc.StartOption{
			grpc.WithGateway(pb.RegisterHelloHandlerFromEndpoint, grpclib.WithTransportCredentials(insecure.NewCredentials())),
			grpc.WithHTTPServer(otherHTTP),
			grpc.WithHTTP(handleHelloWorld{}),
			grpc.WithErrConverter(errConverter),
			//grpc.WithHealth(hs),
			grpc.WithServerOptions(grpclib.MaxConcurrentStreams(10)),
		}
		if test.tlsCerts.Certificate != nil {
			opts = append(opts, grpc.WithTLS([]tls.Certificate{test.tlsCerts}))
		}

		err = server.Start(
			context.Background(),
			lis,
			opts...,
		)
		if err != nil {
			t.Fatalf("TestMostOptions(%s): %s", test.name, err)
		}
		defer server.Stop(context.Background(), false)

		time.Sleep(1 * time.Second) // Wait for server startup

		gDialOpt := []grpclib.DialOption{grpclib.WithBlock()}
		if test.tlsCerts.Certificate == nil {
			gDialOpt = append(gDialOpt, grpclib.WithInsecure())
		} else {
			tlsconf := &tls.Config{InsecureSkipVerify: true}
			creds := credentials.NewTLS(tlsconf)
			gDialOpt = append(gDialOpt, grpclib.WithTransportCredentials(creds))
		}
		log.Println("dial grpc")
		clientConn, err := grpclib.NewClient(lis.Addr().String(), gDialOpt...)
		if err != nil {
			panic(err)
		}

		// Test GRPC.
		client := pb.NewHelloClient(clientConn)

		resp, err := client.Hello(context.Background(), &pb.HelloReq{Name: "John"})
		if err != nil {
			t.Fatalf("TestMostOptions(%s)(grpc call): %s", test.name, err)
		}
		if resp.Msg != "Hello, John!" {
			t.Fatalf("TestMostOptions(%s)(grpc call): expected %q, got %q", "Hello, John!", test.name, resp.Msg)
		}

		// Test err conversion works.
		resp, err = client.Hello(context.Background(), &pb.HelloReq{})
		if err == nil {
			t.Fatalf("TestMostOptions(%s)(grpc call): expected err, got nil", test.name)
		}
		if status.Code(err) != codes.Internal {
			t.Fatalf("TestMostOptions(%s)(grpc call): want err code %v, got %v", test.name, codes.Internal, status.Code(err))
		}

		// Test GRPC Gateway.
		log.Println("dial rest")
		rest := newREST(lis.Addr().String(), test.tlsCerts.Certificate != nil)
		defer rest.close()

		resp, err = rest.restGRPCCall(context.Background(), &pb.HelloReq{Name: "John"})
		if err != nil {
			t.Fatalf("TestMostOptions(%s)(rest call): %s", test.name, err)
		}

		if resp.Msg != "Hello, John!" {
			t.Fatalf("TestMostOptions(%s)(rest call): expected %q, got %q", "Hello, John!", test.name, resp.Msg)
		}

		// Test HTTP.
		log.Println("dial http")
		hClient := &http.Client{}
		scheme := "http"
		if test.tlsCerts.Certificate != nil {
			scheme = "https"
			hClient.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
		}
		req, err := http.NewRequest("GET", fmt.Sprintf("%s://%s", scheme, lis.Addr().String()), nil)
		if err != nil {
			t.Fatalf("TestMostOptions(%s)(http call): %s", test.name, err)
		}
		hResp, err := hClient.Do(req)
		if err != nil {
			t.Fatalf("TestMostOptions(%s)(http call): %s", test.name, err)
		}
		defer hResp.Body.Close()
		b, _ := io.ReadAll(hResp.Body)
		if string(b) != "Hello, World!" {
			t.Fatalf("TestMostOptions(%s)(http call): expected %q, got %q", "Hello, World!", test.name, string(b))
		}
	}
}

var otherHTTP = &http.Server{}

type handleHelloWorld struct {
}

func (h handleHelloWorld) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello, World!"))
}
