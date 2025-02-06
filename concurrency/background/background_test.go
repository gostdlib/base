package background

import (
	"context"
	"errors"
	"log"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/retry/exponential"
)

var noBackoff *exponential.Backoff

func init() {
	var err error
	noBackoff, err = exponential.New(
		exponential.WithPolicy(
			exponential.Policy{
				InitialInterval:     1,
				MaxInterval:         2,
				Multiplier:          1.1,
				RandomizationFactor: 0,
			},
		),
	)
	if err != nil {
		panic(err)
	}
}

func TestBackgroundBasics(t *testing.T) {
	t.Parallel()

	b := New(context.Background())
	defer b.Close(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	count := atomic.Uint64{}
	b.Run(
		ctx,
		"TestBackground-func1",
		func(ctx context.Context) error {
			for {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				count.Add(1)
				time.Sleep(100 * time.Millisecond)
			}
		},
		noBackoff,
	)

	time.Sleep(500 * time.Millisecond)
	if count.Load() < 2 {
		t.Fatalf("TestBackground: expected function to have looped at least twice")
	}
	cancel()
	time.Sleep(200 * time.Millisecond)
	v := count.Load()
	time.Sleep(200 * time.Millisecond)
	if v != count.Load() {
		t.Fatalf("TestBackground: looks like the function didn't exit")
	}
}

func TestBackgroundWhenCrashing(t *testing.T) {
	t.Parallel()

	b := New(context.Background())
	defer b.Close(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	count := atomic.Uint64{}
	b.Run(
		ctx,
		"TestBackground-func2",
		func(ctx context.Context) error {
			for {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				count.Add(1)
				time.Sleep(100 * time.Millisecond)
				return errors.New("crap")
			}
		},
		noBackoff,
	)

	time.Sleep(500 * time.Millisecond)
	if count.Load() < 2 {
		t.Fatalf("TestBackgroundWhenCrashing: expected function to have looped at least twice")
	}
	cancel()
	time.Sleep(200 * time.Millisecond)
	v := count.Load()
	time.Sleep(200 * time.Millisecond)
	if v != count.Load() {
		t.Fatalf("TestBackgroundWhenCrashing: looks like the function didn't exit")
	}
}

func TestBackgroundWithBackoff(t *testing.T) {
	t.Parallel()

	_10Sec, err := exponential.New(
		exponential.WithPolicy(
			exponential.Policy{
				InitialInterval:     10 * time.Second,
				MaxInterval:         10 * time.Second,
				Multiplier:          1.1,
				RandomizationFactor: 0,
			},
		),
	)
	if err != nil {
		panic(err)
	}

	b := New(context.Background())
	defer b.Close(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	count := atomic.Uint64{}
	b.Run(
		ctx,
		"TestBackground-func3",
		func(ctx context.Context) error {
			for {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				count.Add(1)
				log.Println("hello")
				return errors.New("crap")
			}
		},
		_10Sec,
	)

	time.Sleep(5 * time.Second)
	if count.Load() != 1 {
		t.Fatalf("TestBackgroundWhenCrashing: expected function to have not retried yet(was %d)", count.Load())
	}
	time.Sleep(6 * time.Second)
	cancel()
	if count.Load() != 2 {
		t.Fatalf("TestBackgroundWhenCrashing: its ignoring the backoff")
	}
}

func TestOnce(t *testing.T) {
	t.Parallel()

	b := New(context.Background())
	defer b.Close(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	b.Once(
		ctx,
		"TestOnce-func1",
		func(ctx context.Context) error {
			close(done)
			return nil
		},
	)

	<-done
}

func TestCloseContextExpires(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "No deadline, defaults to 30 seconds",
			ctx:  context.Background(),
		},
		{
			name: "Deadline in 5 seconds",
			ctx: func() context.Context {
				// ignore the lostcancel note.
				ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
				return ctx
			}(),
		},
	}

	closer := make(chan struct{})
	b := New(context.Background())
	b.Run(
		context.Background(),
		"TestBackground-func4",
		func(ctx context.Context) error {
			<-closer
			return nil
		},
		noBackoff,
	)

	for _, test := range tests {
		if err := b.Close(test.ctx); err == nil {
			t.Fatalf("TestCloseContextExpires(%s): expected an error, got nil", test.name)
		}
	}

	close(closer)
}
