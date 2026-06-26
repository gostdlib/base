package background

import (
	"context"
	"errors"
	"log"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gostdlib/base/concurrency/worker"

	"github.com/Azure/retry/exponential"
)

// bubble runs f inside a testing/synctest bubble after installing a fresh worker pool as the
// default pool. background.New() draws its goroutines from worker.Default(); creating that pool
// inside the bubble is what lets the bubble's fake clock control the background tasks' timers.
// The previous default pool is restored when the bubble exits.
func bubble(t *testing.T, f func(t *testing.T)) {
	t.Helper()

	original := worker.Default()
	defer worker.Set(original)

	synctest.Test(t, func(t *testing.T) {
		bp, err := worker.New(context.Background(), "bubblePool")
		if err != nil {
			t.Fatalf("%s: creating bubble pool: %s", t.Name(), err)
		}
		worker.Set(bp)
		defer bp.Close(context.Background())

		f(t)
	})
}

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

func TestBackground(t *testing.T) {
	tests := []struct {
		name  string
		crash bool
	}{
		{
			name:  "Success: non-crashing task loops then stops on cancel",
			crash: false,
		},
		{
			name:  "Success: crashing task is restarted by backoff then stops on cancel",
			crash: true,
		},
	}

	for _, test := range tests {
		bubble(t, func(t *testing.T) {
			b := New(context.Background())
			defer b.Close(context.Background())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			count := atomic.Uint64{}
			b.Run(
				ctx,
				"TestBackground-task",
				func(ctx context.Context) error {
					for {
						if ctx.Err() != nil {
							return ctx.Err()
						}
						count.Add(1)
						time.Sleep(100 * time.Millisecond)
						// A crashing task returns an error each iteration; noBackoff then restarts it,
						// which must keep the count climbing just like the internally-looping task.
						if test.crash {
							return errors.New("crap")
						}
					}
				},
				noBackoff,
			)

			time.Sleep(500 * time.Millisecond)
			if count.Load() < 2 {
				t.Fatalf("TestBackground(%s): expected function to have looped at least twice", test.name)
			}
			cancel()
			// Advance the fake clock past the task's in-flight Sleep so it observes the cancellation,
			// then wait for the task goroutine to settle. The count must not change after that.
			time.Sleep(100 * time.Millisecond)
			synctest.Wait()
			v := count.Load()
			synctest.Wait()
			if v != count.Load() {
				t.Fatalf("TestBackground(%s): looks like the function didn't exit", test.name)
			}
		})
	}
}

func TestBackgroundWithBackoff(t *testing.T) {
	bubble(t, func(t *testing.T) {
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
				if ctx.Err() != nil {
					return ctx.Err()
				}
				count.Add(1)
				log.Println("hello")
				return errors.New("crap")
			},
			_10Sec,
		)

		// Each Sleep advances the bubble's fake clock; the 10s backoff is what we are verifying.
		// No synctest.Wait() is needed: the retry timer (T=10s) fires strictly before the main
		// goroutine's next wake (T=11s), so the worker has already run and re-settled by then.
		time.Sleep(5 * time.Second)
		if count.Load() != 1 {
			t.Fatalf("TestBackgroundWhenCrashing: expected function to have not retried yet(was %d)", count.Load())
		}
		time.Sleep(6 * time.Second)
		cancel()
		if count.Load() != 2 {
			t.Fatalf("TestBackgroundWhenCrashing: its ignoring the backoff")
		}
	})
}

func TestOnce(t *testing.T) {
	// Not parallel: bubble() replaces the global worker.Default() for synctest determinism.

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

// TestRunOnceCanceledContext verifies that Run and Once report an error (and do not start the
// task) when the context is already canceled before the task can be submitted, and report nil
// on a live context. This exercises the bool returned by the underlying pool.Submit.
func TestRunOnceCanceledContext(t *testing.T) {
	task := func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}

	tests := []struct {
		name    string
		once    bool
		cancel  bool
		wantErr bool
	}{
		{name: "Success: Run with a live context starts the task", wantErr: false},
		{name: "Success: Once with a live context starts the task", once: true, wantErr: false},
		{name: "Error: Run with a canceled context does not start the task", cancel: true, wantErr: true},
		{name: "Error: Once with a canceled context does not start the task", once: true, cancel: true, wantErr: true},
	}

	for _, test := range tests {
		b := New(context.Background())

		ctx, cancel := context.WithCancel(context.Background())
		if test.cancel {
			cancel()
		}

		var err error
		if test.once {
			err = b.Once(ctx, "TestRunOnce-once", task)
		} else {
			err = b.Run(ctx, "TestRunOnce-run", task, noBackoff)
		}

		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestRunOnceCanceledContext(%s): got err == nil, want err != nil", test.name)
		case err != nil && !test.wantErr:
			t.Errorf("TestRunOnceCanceledContext(%s): got err == %s, want err == nil", test.name, err)
		}

		cancel()
		b.Close(context.Background())
	}
}

func TestCloseContextExpires(t *testing.T) {
	// Not parallel: bubble() replaces the global worker.Default() for synctest determinism.

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
				ctx, _ := context.WithTimeout(t.Context(), 5*time.Second)
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
