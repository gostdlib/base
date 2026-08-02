package worker

import (
	"testing"
)

func TestSet(t *testing.T) {
	ctx := t.Context()

	original := Default()
	defer Set(original)

	base, err := New(ctx, "test")
	if err != nil {
		t.Fatalf("TestSet: creating test pool: %s", err)
	}
	defer base.Close(ctx)

	tests := []struct {
		name      string
		pool      *Pool
		wantPanic bool
	}{
		{
			name: "Success: unlimited pool is stored",
			pool: base,
		},
		{
			name:      "Error: nil pool panics",
			pool:      nil,
			wantPanic: true,
		},
		{
			name:      "Error: limited pool panics",
			pool:      base.Limited(ctx, "limited", 2),
			wantPanic: true,
		},
	}

	for _, test := range tests {
		panicked := func() (p bool) {
			defer func() {
				if r := recover(); r != nil {
					p = true
				}
			}()
			Set(test.pool)
			return false
		}()

		switch {
		case panicked && !test.wantPanic:
			t.Errorf("TestSet(%s): got panic, want no panic", test.name)
			continue
		case !panicked && test.wantPanic:
			t.Errorf("TestSet(%s): got no panic, want panic", test.name)
			continue
		case panicked:
			continue
		}

		if Default() != test.pool {
			t.Errorf("TestSet(%s): Default() did not return the pool passed to Set()", test.name)
		}
	}
}
