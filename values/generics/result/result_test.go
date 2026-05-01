package result

import (
	"errors"
	"testing"
	"time"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/context"
)

func TestNew(t *testing.T) {
	t.Parallel()

	r := New[int]()
	if r == nil {
		t.Fatalf("TestNew: got nil, want non-nil *Value")
	}
	if r.Done() == nil {
		t.Fatalf("TestNew: got nil Done channel, want non-nil")
	}
	select {
	case <-r.Done():
		t.Fatalf("TestNew: Done channel closed before Set was called")
	default:
	}
}

func TestSet(t *testing.T) {
	t.Parallel()

	setErr := errors.New("set error")

	tests := []struct {
		name    string
		v       int
		err     error
		wantErr bool
	}{
		{
			name:    "Success: non-zero value with nil error",
			v:       42,
			err:     nil,
			wantErr: false,
		},
		{
			name:    "Success: zero value with nil error",
			v:       0,
			err:     nil,
			wantErr: false,
		},
		{
			name:    "Error: zero value with error",
			v:       0,
			err:     setErr,
			wantErr: true,
		},
	}

	for _, test := range tests {
		r := New[int]()
		r.Set(test.v, test.err)

		select {
		case <-r.Done():
		default:
			t.Errorf("TestSet(%s): Done channel not closed after Set", test.name)
			continue
		}

		gotV, gotErr := r.Wait(t.Context())

		switch {
		case gotErr == nil && test.wantErr:
			t.Errorf("TestSet(%s): got err == nil, want err != nil", test.name)
			continue
		case gotErr != nil && !test.wantErr:
			t.Errorf("TestSet(%s): got err == %s, want err == nil", test.name, gotErr)
			continue
		case gotErr != nil:
			if gotErr != test.err {
				t.Errorf("TestSet(%s): got err == %v, want err == %v", test.name, gotErr, test.err)
			}
			continue
		}

		if gotV != test.v {
			t.Errorf("TestSet(%s): got v == %d, want v == %d", test.name, gotV, test.v)
		}
	}
}

func TestSetTwicePanics(t *testing.T) {
	t.Parallel()

	r := New[int]()
	r.Set(1, nil)

	defer func() {
		if rec := recover(); rec == nil {
			t.Errorf("TestSetTwicePanics: got no panic, want panic on second Set")
		}
	}()
	r.Set(2, nil)
}

func TestWaitBlocksUntilSet(t *testing.T) {
	t.Parallel()

	r := New[string]()
	want := "answer"

	go func() {
		time.Sleep(20 * time.Millisecond)
		r.Set(want, nil)
	}()

	got, err := r.Wait(t.Context())
	if err != nil {
		t.Fatalf("TestWaitBlocksUntilSet: got err == %s, want err == nil", err)
	}
	if got != want {
		t.Errorf("TestWaitBlocksUntilSet: got v == %q, want v == %q", got, want)
	}
}

func TestWaitContextCanceled(t *testing.T) {
	t.Parallel()

	r := New[int]()
	cause := errors.New("canceled by test")

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)

	gotV, gotErr := r.Wait(ctx)
	if gotErr == nil {
		t.Fatalf("TestWaitContextCanceled: got err == nil, want err != nil")
	}
	if !errors.Is(gotErr, cause) {
		t.Errorf("TestWaitContextCanceled: got err == %v, want errors.Is(err, %v)", gotErr, cause)
	}
	if gotV != 0 {
		t.Errorf("TestWaitContextCanceled: got v == %d, want v == 0", gotV)
	}

	select {
	case <-r.Done():
		t.Errorf("TestWaitContextCanceled: Done channel closed without Set")
	default:
	}
}

func TestConcurrentWait(t *testing.T) {
	t.Parallel()

	type payload struct {
		ID   int
		Name string
	}

	r := New[payload]()
	want := payload{ID: 7, Name: "lucky"}

	const waiters = 50

	g := sync.Group{}
	for i := 0; i < waiters; i++ {
		g.Go(t.Context(), func(ctx context.Context) error {
			got, err := r.Wait(ctx)
			if err != nil {
				return err
			}
			if got != want {
				t.Errorf("TestConcurrentWait: got v == %+v, want v == %+v", got, want)
			}
			return nil
		})
	}

	time.Sleep(10 * time.Millisecond)
	r.Set(want, nil)

	if err := g.Wait(t.Context()); err != nil {
		t.Errorf("TestConcurrentWait: got err == %s, want err == nil", err)
	}
}
