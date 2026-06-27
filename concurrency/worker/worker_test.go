package worker

import (
	"bytes"
	"context"
	"log/slog"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gostdlib/base/telemetry/log"
)

func TestPool(t *testing.T) {
	t.Parallel()

	// Exercise both a named (metrics-enabled) and an anonymous pool. The pool must be created
	// inside runPool's synctest bubble, so we pass the name rather than a pre-built pool.
	runPool(t, "myPool")
	runPool(t, "")
}

func runPool(t *testing.T, name string) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		p, err := New(ctx, name)
		if err != nil {
			panic(err)
		}
		defer p.Close(ctx)

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

		// During the submit burst the fake clock is frozen (the main goroutine never blocks), so
		// every runner is stuck in its job's Sleep and each excess submit must spawn a new
		// goroutine. The pool must therefore have grown beyond NumCPU. This replaces the original
		// busy-loop poller, which never durably blocks and is incompatible with synctest.
		if got := p.GoRoutines(); got <= runtime.NumCPU() {
			t.Fatalf("TestPool: pool did not grow as expected: GoRoutines()=%d, NumCPU=%d", got, runtime.NumCPU())
		}

		p.Wait()

		for i, e := range answer {
			if !e {
				t.Fatalf("TestPool: entry(%d) was not set to true as expected", i)
			}
		}

		// A goroutine beyond the static pool that is idle for the 1s runner timeout is collected.
		// Advancing the fake clock by two seconds collects them all, leaving exactly NumCPU.
		time.Sleep(2 * time.Second)
		if p.GoRoutines() != runtime.NumCPU() {
			t.Fatalf("TestPool(number of goroutines): got %d, want %d", p.GoRoutines(), runtime.NumCPU())
		}
	})
}

func TestSubPool(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		p, err := New(ctx, "myPool")
		if err != nil {
			panic(err)
		}
		defer p.Close(ctx)

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

		time.Sleep(2 * time.Second) // Wait for extra goroutines to timeout (fake clock).

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
	})
}

// TestSubPoolName is a regression test for Sub() dropping the pool name. Because Sub() built the
// child pool without copying name, every Sub()/Limited() pool reported as "unnamed" in the
// "waiting more than 30 seconds to acquire limited pool slot" warning (see limitedSubmit), making
// it impossible to tell which limited pool was starving.
func TestSubPoolName(t *testing.T) {
	ctx := context.Background()
	p, err := New(ctx, "root")
	if err != nil {
		t.Fatalf("TestSubPoolName: got err == %s, want err == nil", err)
	}
	defer p.Close(ctx)

	tests := []struct {
		name string
		pool *Pool
		want string
	}{
		{name: "Success: Sub propagates the name", pool: p.Sub(ctx, "subby"), want: "subby"},
		{name: "Success: Limited propagates the name", pool: p.Limited(ctx, "limity", 1), want: "limity"},
		{name: "Success: empty name stays empty", pool: p.Sub(ctx, ""), want: ""},
	}

	for _, test := range tests {
		if test.pool.name != test.want {
			t.Errorf("TestSubPoolName(%s): got name == %q, want name == %q", test.name, test.pool.name, test.want)
		}
	}
}

func TestRunningZeroAfterWait(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	// The bug: done() calls wg.Done() before running.Add(-1). This means
	// Wait() (which calls wg.Wait()) can return while Running() is still > 0.
	// We use runtime.Gosched() in the submitted work to increase the chance
	// of the scheduler switching between wg.Done() and running.Add(-1).
	failed := false
	for iter := 0; iter < 5000; iter++ {
		p, err := New(ctx, "", WithSize(1))
		if err != nil {
			t.Fatalf("TestRunningZeroAfterWait: got err == %s, want err == nil", err)
		}

		p.Submit(ctx, func() {
			runtime.Gosched()
		})
		p.Wait()

		if got := p.Running(); got != 0 {
			t.Errorf("TestRunningZeroAfterWait(iter %d): got Running() == %d, want 0 after Wait()", iter, got)
			failed = true
			p.Close(ctx)
			break
		}
		p.Close(ctx)
	}
	if !failed {
		t.Log("TestRunningZeroAfterWait: race did not trigger in 5000 iterations (may need fix anyway)")
	}
}

func TestSubPoolDoesNotInflateGoRoutines(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		size := 4
		p, err := New(ctx, "inflateTest", WithSize(size))
		if err != nil {
			t.Fatalf("TestSubPoolDoesNotInflateGoRoutines: got err == %s, want err == nil", err)
		}
		defer p.Close(ctx)

		// After creation, the pool should have exactly `size` goroutines. Wait for the static
		// runners to start and durably block on the queue before reading the baseline.
		synctest.Wait()
		baseline := p.GoRoutines()

		// Create multiple sub pools. Each Sub() call should NOT inflate the counter.
		sub1 := p.Sub(ctx, "sub1")
		sub2 := p.Sub(ctx, "sub2")
		sub3 := p.Sub(ctx, "sub3")

		afterSubs := p.GoRoutines()

		// The bug: each Sub() adds p.opts.size to the shared goRoutines counter,
		// so after 3 Sub() calls the counter is inflated by 3 * size.
		// The correct behavior is that Sub() should not change the counter at all
		// since no new goroutines are created.
		if afterSubs != baseline {
			t.Errorf("TestSubPoolDoesNotInflateGoRoutines: GoRoutines() changed after Sub() calls: baseline %d, after 3 Sub() calls %d (inflated by %d)", baseline, afterSubs, afterSubs-baseline)
		}

		// Verify the subs share the same counter value.
		if sub1.GoRoutines() != p.GoRoutines() {
			t.Errorf("TestSubPoolDoesNotInflateGoRoutines: sub1 and parent have different GoRoutines()")
		}
		if sub2.GoRoutines() != p.GoRoutines() {
			t.Errorf("TestSubPoolDoesNotInflateGoRoutines: sub2 and parent have different GoRoutines()")
		}
		if sub3.GoRoutines() != p.GoRoutines() {
			t.Errorf("TestSubPoolDoesNotInflateGoRoutines: sub3 and parent have different GoRoutines()")
		}
	})
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

		synctest.Test(t, func(t *testing.T) {
			ctx := context.Background()

			var p *Pool
			var err error
			if test.disableWarn {
				p, err = New(ctx, "testPool", WithDisableLimitedWarn(true))
			} else {
				p, err = New(ctx, "testPool")
			}
			if err != nil {
				t.Errorf("TestLimitedPoolWarning(%s): got err == %s, want err == nil", test.name, err)
				return
			}
			defer p.Close(ctx)

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

			// Let the second submit reach its blocked state (the single slot is taken), then
			// advance the fake clock past warnTimer so the waiting submit logs its warning (only
			// when warnings are enabled).
			synctest.Wait()
			time.Sleep(warnTimer + time.Millisecond)
			synctest.Wait()

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
		})

		log.Set(originalLogger)
	}
}
