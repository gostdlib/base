package detect

import (
	"context"
	"os"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInit(t *testing.T) {
	KUBERNETES_SERVICE_HOST := os.Getenv("KUBERNETES_SERVICE_HOST")
	KUBERNETES_SERVICE_PORT := os.Getenv("KUBERNETES_SERVICE_PORT")

	t.Cleanup(
		func() {
			os.Setenv("KUBERNETES_SERVICE_HOST", KUBERNETES_SERVICE_HOST)
			os.Setenv("KUBERNETES_SERVICE_PORT", KUBERNETES_SERVICE_PORT)
		},
	)
	os.Setenv("KUBERNETES_SERVICE_HOST", "something")
	os.Setenv("KUBERNETES_SERVICE_PORT", "127.0.0.1:8276")

	baseIs = is{
		testKind: func(ctx context.Context) (bool, error) {
			return false, nil
		},
	}
	t.Cleanup(func() { baseIs = is{} })

	Init()

	if !base.alreadyRun {
		t.Fatalf("Init() did not run")
	}

	if !base.IsKubernetes {
		t.Fatalf("Init() did not set IsKubernetes")
	}

	if base.Err != nil {
		t.Fatalf("Init() failed with %v", base.Err)
	}

	if base.IsKind {
		t.Fatalf("Init() set IsKind")
	}
}

func TestIsK8(t *testing.T) {
	KUBERNETES_SERVICE_HOST := os.Getenv("KUBERNETES_SERVICE_HOST")

	t.Cleanup(
		func() {
			os.Setenv("KUBERNETES_SERVICE_HOST", KUBERNETES_SERVICE_HOST)
		},
	)

	tests := []struct {
		name    string
		setHost bool
		want    bool
	}{
		{
			name: "KUBERNETES_SERVICE_HOST empty",
		},
		{
			name:    "KUBERNETES_SERVICE_HOST set",
			setHost: true,
			want:    true,
		},
	}

	for _, test := range tests {
		if test.setHost {
			os.Setenv("KUBERNETES_SERVICE_HOST", "something")
		} else {
			os.Setenv("KUBERNETES_SERVICE_HOST", "")
		}

		if got := (is{}).k8(context.Background()); got != test.want {
			t.Errorf("TestIsK8(%s): got %t, want %t", test.name, got, test.want)
		}
	}
}

func TestDetectKindFromNodes(t *testing.T) {
	tests := []struct {
		name  string
		nodes *v1.NodeList
		want  bool
	}{
		{
			name: "nil nodes",
		},
		{
			name:  "empty nodes",
			nodes: &v1.NodeList{},
		},
		{
			name: "has kind label",
			nodes: &v1.NodeList{
				Items: []v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"kind.x-k8s.io/cluster": "world",
							},
						},
					},
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		got := is{}.detectKindFromNodes(test.nodes)
		if got != test.want {
			t.Errorf("TestDetectKindFromNodes(%s): got %t, want %t", test.name, got, test.want)
		}
	}
}
