package worker

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestSubmitContextCancellation guards the documented Submit contract: "If the context is
// canceled before the function is enqueued, the function will not be executed." Both the
// regular submit path and the limitedSubmit path previously violated this because their
// select statements raced <-ctx.Done() against the enqueue/acquire case, so a cancelled-context
// job ran a fraction of the time. We submit many jobs to make the regression deterministic:
// buggy code runs a non-zero number of cancelled jobs across the iterations, while the correct
// behavior runs exactly zero. Live-context cases confirm jobs still run normally.
func TestSubmitContextCancellation(t *testing.T) {
	const iters = 2000

	tests := []struct {
		name      string
		limited   bool
		cancelCtx bool
		wantRuns  int64
	}{
		{
			name:     "Success: live context runs the job on a regular pool",
			wantRuns: iters,
		},
		{
			name:     "Success: live context runs the job on a limited pool",
			limited:  true,
			wantRuns: iters,
		},
		{
			name:      "Error: cancelled context never runs the job on a regular pool",
			cancelCtx: true,
			wantRuns:  0,
		},
		{
			name:      "Error: cancelled context never runs the job on a limited pool",
			limited:   true,
			cancelCtx: true,
			wantRuns:  0,
		},
	}

	for _, test := range tests {
		ctx := t.Context()

		p, err := New(ctx, "")
		if err != nil {
			t.Fatalf("TestSubmitContextCancellation(%s): New: %s", test.name, err)
		}

		submitter := p
		if test.limited {
			submitter = p.Limited(ctx, "", 4)
		}

		var runs atomic.Int64
		var accepted int64
		for i := 0; i < iters; i++ {
			jobCtx := ctx
			if test.cancelCtx {
				c, cancel := context.WithCancel(ctx)
				cancel()
				jobCtx = c
			}
			if submitter.Submit(jobCtx, func() { runs.Add(1) }) {
				accepted++
			}
		}
		submitter.Wait()
		p.Close(ctx)

		if got := runs.Load(); got != test.wantRuns {
			t.Errorf("TestSubmitContextCancellation(%s): jobs run = %d, want %d", test.name, got, test.wantRuns)
		}
		// Submit's bool return must reflect whether the job was accepted to run: every accepted
		// (true) submit runs, and every declined (false) submit does not.
		if accepted != test.wantRuns {
			t.Errorf("TestSubmitContextCancellation(%s): Submit returned true %d times, want %d", test.name, accepted, test.wantRuns)
		}
	}
}
