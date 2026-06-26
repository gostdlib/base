package sync

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDecliningPool mirrors worker.Pool's documented Submit contract: a job submitted with an
// already-cancelled context is declined (not run) and Submit returns false; otherwise it runs
// the job and returns true.
type fakeDecliningPool struct{}

func (fakeDecliningPool) Submit(ctx context.Context, f func()) bool {
	if ctx.Err() != nil {
		return false
	}
	go f()
	return true
}

// TestGroupDeclinedSubmit guards against a regression where a Group backed by a pool deadlocked
// in Wait(): execute() calls wg.Add(1), but when Submit declines the job (cancelled context) the
// matching wg.Done() never happens because the job never runs. The fix unwinds the accounting and
// records the cause when Submit returns false.
func TestGroupDeclinedSubmit(t *testing.T) {
	tests := []struct {
		name      string
		cancelCtx bool
		wantRan   int32
		wantErr   bool
	}{
		{
			name:    "Success: live context runs the job and Wait returns nil",
			wantRan: 1,
			wantErr: false,
		},
		{
			name:      "Error: cancelled context declines the job; Wait returns the cause and does not hang",
			cancelCtx: true,
			wantRan:   0,
			wantErr:   true,
		},
	}

	for _, test := range tests {
		grp := Group{Pool: fakeDecliningPool{}}

		jobCtx := context.Background()
		if test.cancelCtx {
			c, cancel := context.WithCancel(context.Background())
			cancel()
			jobCtx = c
		}

		var ran atomic.Int32
		grp.Go(jobCtx, func(ctx context.Context) error {
			ran.Add(1)
			return nil
		})

		// Wait() must not be cancellable, so it gets its own live context. Run it off the test
		// goroutine so a regression (the old deadlock) is reported as a failure instead of hanging
		// the whole test binary.
		waitErr := make(chan error, 1)
		go func() {
			waitErr <- grp.Wait(context.Background())
		}()

		var err error
		select {
		case err = <-waitErr:
		case <-time.After(5 * time.Second):
			t.Errorf("TestGroupDeclinedSubmit(%s): Group.Wait() deadlocked (Submit-declined job left wg.Add unmatched)", test.name)
			continue
		}

		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestGroupDeclinedSubmit(%s): got err == nil, want err != nil", test.name)
		case err != nil && !test.wantErr:
			t.Errorf("TestGroupDeclinedSubmit(%s): got err == %s, want err == nil", test.name, err)
		}

		if got := ran.Load(); got != test.wantRan {
			t.Errorf("TestGroupDeclinedSubmit(%s): job ran %d times, want %d", test.name, got, test.wantRan)
		}
	}
}
