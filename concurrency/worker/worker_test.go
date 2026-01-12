package worker

import (
	"bytes"
	"context"
	"log/slog"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gostdlib/base/telemetry/log"
)

func TestPool(t *testing.T) {
	t.Parallel()

	namedPool, err := New(t.Context(), "myPool")
	if err != nil {
		panic(err)
	}
	runPool(namedPool, t)

	anonPool, err := New(t.Context(), "")
	if err != nil {
		panic(err)
	}
	runPool(anonPool, t)
}

func runPool(p *Pool, t *testing.T) {
	ctx := context.Background()
	p, err := New(ctx, "myPool")
	if err != nil {
		panic(err)
	}

	verifyPoolGrowth := make(chan bool, 1)
	go func() {
		for {
			if p.goRoutines.Load() > int64(runtime.NumCPU()) {
				verifyPoolGrowth <- true
				return
			}
		}
	}()

	answer := make([]bool, 1000)
	for i := 0; i < 1000; i++ {
		i := i
		p.Submit(
			ctx,
			func() {
				answer[i] = true
				time.Sleep(100 * time.Millisecond)
			},
		)
	}
	p.Wait()

	for i, e := range answer {
		if !e {
			t.Fatalf("TestPool: entry(%d) was not set to true as expected", i)
		}
	}

	// An extra goroutine that isn't used for 1 second is collected by the pool. By waiting two seconds,
	// we should see that the number of goroutines is equal to the number of CPUs.
	time.Sleep(2 * time.Second)
	if p.GoRoutines() != runtime.NumCPU() {
		t.Fatalf("TestPool(number of goroutines): got %d, want %d", p.GoRoutines(), runtime.NumCPU())
	}
	select {
	case <-verifyPoolGrowth:
	default:
		t.Fatalf("TestPool: pool did not grow as expected")
	}
}

func TestSubPool(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	p, err := New(ctx, "myPool")
	if err != nil {
		panic(err)
	}

	sub1 := p.Sub(ctx, "subPool1")
	sub2 := sub1.Sub(ctx, "subPool2")

	answer := make([]bool, 1000)
	for i := 0; i < 1000; i++ {
		if i%3 == 0 {
			p.Submit(
				ctx,
				func() {
					answer[i] = true
					time.Sleep(100 * time.Millisecond)
				},
			)
		} else if i%3 == 1 {
			sub1.Submit(
				ctx,
				func() {
					answer[i] = true
					time.Sleep(100 * time.Millisecond)
				},
			)
		} else {
			sub2.Submit(
				ctx,
				func() {
					answer[i] = true
					time.Sleep(100 * time.Millisecond)
				},
			)
		}
	}

	p.Wait()
	sub1.Wait()
	sub2.Wait()

	time.Sleep(2 * time.Second) // Wait for extra goroutines to timeout.

	for i, e := range answer {
		if !e {
			t.Fatalf("TestSubPool: entry(%d) was not set to true as expected", i)
		}
	}

	if p.GoRoutines() != sub1.GoRoutines() {
		t.Fatalf("TestSubPool: pool and sub1 did not have the same number of goroutines")
	}

	if p.GoRoutines() != sub2.GoRoutines() {
		t.Fatalf("TestSubPool: pool and sub2 did not have the same number of goroutines")
	}

	if p.StaticPool() != sub1.StaticPool() {
		t.Fatal("TestSubPool: pool and sub1 did not have the same number of static pool goroutines")
	}
	if p.StaticPool() != sub2.StaticPool() {
		t.Fatal("TestSubPool: pool and sub2 did not have the same number of static pool goroutines")
	}

	sub2.Close(ctx)

	at := atomic.Int64{}
	sub1.Submit(
		ctx,
		func() {
			at.Add(1)
		},
	)
	p.Submit(
		ctx,
		func() {
			at.Add(1)
		},
	)
	sub1.Wait()
	p.Wait()
	if at.Load() != 2 {
		t.Fatal("TestSubPool: closing sub2 did something bad")
	}
}

func TestLimitedPoolWarning(t *testing.T) {
	// Save and restore original warnTimer
	originalWarnTimer := warnTimer
	defer func() { warnTimer = originalWarnTimer }()

	// Set short timeout for testing
	warnTimer = 50 * time.Millisecond

	tests := []struct {
		name        string
		disableWarn bool
		wantWarn    bool
	}{
		{
			name:        "Success: warning logged when enabled",
			disableWarn: false,
			wantWarn:    true,
		},
		{
			name:        "Success: no warning when disabled",
			disableWarn: true,
			wantWarn:    false,
		},
	}

	for _, test := range tests {
		// Capture log output
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
		testLogger := slog.New(handler)

		// Save and restore original logger
		originalLogger := log.Default()
		log.Set(testLogger)

		ctx := t.Context()

		var p *Pool
		var err error
		if test.disableWarn {
			p, err = New(ctx, "testPool", WithDisableLimitedWarn(true))
		} else {
			p, err = New(ctx, "testPool")
		}
		if err != nil {
			t.Errorf("TestLimitedPoolWarning(%s): got err == %s, want err == nil", test.name, err)
			log.Set(originalLogger)
			continue
		}

		// Create a limited pool with only 1 slot
		limited := p.Limited(ctx, "limitedPool", 1)

		// Track completion
		firstDone := make(chan struct{})
		secondDone := make(chan struct{})

		// Fill the slot with a job that blocks until we release it
		release := make(chan struct{})
		limited.Submit(ctx, func() {
			<-release
			close(firstDone)
		})

		// Submit second job that will have to wait
		go func() {
			limited.Submit(ctx, func() {
				close(secondDone)
			})
		}()

		// Wait for warning to potentially be logged
		time.Sleep(100 * time.Millisecond)

		// Release the first job
		close(release)

		// Wait for both jobs to complete
		<-firstDone
		<-secondDone

		// Verify warning was or wasn't logged
		logOutput := buf.String()
		hasWarning := strings.Contains(logOutput, "waiting more than 30 seconds")

		switch {
		case test.wantWarn && !hasWarning:
			t.Errorf("TestLimitedPoolWarning(%s): expected warning in log but got none. Log: %s", test.name, logOutput)
		case !test.wantWarn && hasWarning:
			t.Errorf("TestLimitedPoolWarning(%s): expected no warning but got: %s", test.name, logOutput)
		}

		p.Close(ctx)
		log.Set(originalLogger)
	}
}
