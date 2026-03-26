package converters

import (
	"context"
	"testing"

	pb "github.com/gostdlib/base/errors/example/proto"
)

func TestRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      any
		wantSub  string
		wantZero bool
	}{
		{
			name:    "Success: nil request returns null",
			req:     nil,
			wantSub: "null",
		},
		{
			name:    "Success: proto message converts to JSON",
			req:     &pb.HelloReq{Name: "world"},
			wantSub: "world",
		},
		{
			name:    "Success: generic struct uses default conversion",
			req:     struct{ Foo string }{Foo: "bar"},
			wantSub: "bar",
		},
		{
			name:    "Success: string uses default conversion",
			req:     "hello",
			wantSub: "hello",
		},
	}

	for _, test := range tests {
		result := Request(context.Background(), test.req)
		if test.wantZero && result != "" {
			t.Errorf("TestRequest(%s): got %q, want empty string", test.name, result)
			continue
		}
		if test.wantSub != "" && !contains(result, test.wantSub) {
			t.Errorf("TestRequest(%s): got %q, want substring %q", test.name, result, test.wantSub)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
