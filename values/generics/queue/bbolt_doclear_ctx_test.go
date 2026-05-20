package queue

import (
	"context"
	"iter"
	"testing"
)

// fakeCtxBackup implements Backup. Push records the ctx it is called with and fails
// if that ctx is canceled, so a test can assert which ctx the bbolt commit pipeline
// hands to the backup mirror.
type fakeCtxBackup struct {
	pushCtxs []context.Context
}

func (b *fakeCtxBackup) Push(ctx context.Context, _ []Number[int]) error {
	b.pushCtxs = append(b.pushCtxs, ctx)
	return ctx.Err()
}
func (b *fakeCtxBackup) Del(context.Context, []Number[int]) error     { return nil }
func (b *fakeCtxBackup) Restore(context.Context, []Number[int]) error { return nil }
func (b *fakeCtxBackup) Len() int64                                   { return 0 }
func (b *fakeCtxBackup) Close(context.Context) error                  { return nil }
func (b *fakeCtxBackup) Clear(context.Context) error                  { return nil }
func (b *fakeCtxBackup) OnLoad(context.Context, Number[int]) error    { return nil }
func (b *fakeCtxBackup) RangeAll(context.Context) iter.Seq2[Number[int], error] {
	return func(yield func(Number[int], error) bool) {}
}

// TestBboltDoClearDrainUsesFlusherCtx is a regression test: doClear's drain step
// commits items pushed by *other* callers, so it must use the flusher's lifetime
// ctx, not the Clear caller's ctx — otherwise a Clear with a canceled ctx fails
// the in-flight buffered Push's commit (via backup.Push), and the unrelated Push
// call returns that cancellation error.
//
// The test calls doClear directly (bypassing the flusher routing, which only
// affects how doClear is *dispatched*, not its drain-ctx choice) with a manually
// buffered item and a canceled Clear ctx. With the fix, backup.Push for the
// drained item sees a non-canceled ctx and the buffered "Push" (its bufCur) gets
// a nil err.
func TestBboltDoClearDrainUsesFlusherCtx(t *testing.T) {
	ctx := t.Context()
	o, err := applyBackingOptions(callBboltFIFO, nil)
	if err != nil {
		t.Fatalf("TestBboltDoClearDrainUsesFlusherCtx: applyBackingOptions got err == %s, want err == nil", err)
	}
	bk, err := newBboltBacking[Number[int]](ctx, diskRoot(t), o, bboltFIFOKey[Number[int]], false)
	if err != nil {
		t.Fatalf("TestBboltDoClearDrainUsesFlusherCtx: newBboltBacking got err == %s, want err == nil", err)
	}
	p := bk.(*bboltBacking[Number[int]])
	// setQueueLock is intentionally not called: the flusher stays offline so doClear
	// is the only goroutine driving commit, and there is no race with a real flush.
	t.Cleanup(func() { _ = p.Close(ctx) })

	backup := &fakeCtxBackup{}
	p.backup = backup

	// Manually stage an item the way a Push would: append to p.buf, bump pending,
	// remember the current cur so we can read its err after the drain.
	p.lk.lock()
	p.buf = []Number[int]{fifoItem(1)}
	p.pending = 1
	bufCur := p.cur
	p.lk.unlock()

	cctx, cancel := context.WithCancel(ctx)
	cancel()

	_ = p.doClear(cctx) // doClear's own backup.Clear/db.Update may run under cctx; not asserted here.

	<-bufCur.done
	if bufCur.err != nil {
		t.Errorf("TestBboltDoClearDrainUsesFlusherCtx: buffered Push got err == %v, want err == nil (doClear's drain leaked the Clear caller's canceled ctx into commit)", bufCur.err)
	}
	if len(backup.pushCtxs) != 1 {
		t.Fatalf("TestBboltDoClearDrainUsesFlusherCtx: backup.Push called %d times, want 1", len(backup.pushCtxs))
	}
	if cerr := backup.pushCtxs[0].Err(); cerr != nil {
		t.Errorf("TestBboltDoClearDrainUsesFlusherCtx: backup.Push was called with a canceled ctx (err=%v); the drain must use the flusher's ctx", cerr)
	}
}
