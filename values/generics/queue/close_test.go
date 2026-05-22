package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestQueueCloseUnblocks verifies that Close unblocks an in-flight Pop (empty queue) and
// Push (full bounded queue) and that they return ErrClosed, across every backing.
func TestQueueCloseUnblocks(t *testing.T) {
	for _, m := range queueMakers() {
		// Blocked Pop on an empty queue.
		func() {
			ctx := t.Context()
			q := m.make(t, ctx, 0)
			errCh := make(chan error, 1)
			started := make(chan struct{})
			go func() {
				close(started) // about to enter Pop
				_, err := q.Pop(ctx, 1)
				errCh <- err
			}()
			// Wait for the worker to reach Pop, then sleep so a non-blocking Pop
			// would land its result on errCh before we Close.
			<-started
			time.Sleep(50 * time.Millisecond)
			if err := q.Close(ctx); err != nil {
				t.Fatalf("TestQueueCloseUnblocks(%s): Close got err == %s, want err == nil", m.name, err)
			}
			select {
			case err := <-errCh:
				if !errors.Is(err, ErrClosed) {
					t.Errorf("TestQueueCloseUnblocks(%s): blocked Pop after Close got err == %v, want ErrClosed", m.name, err)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("TestQueueCloseUnblocks(%s): Pop did not unblock after Close", m.name)
			}
		}()

		// Blocked Push on a full bounded queue.
		func() {
			ctx := t.Context()
			q := m.make(t, ctx, 1)
			if ok, err := q.Push(ctx, []Number[int]{m.item(0)}); err != nil || !ok {
				t.Fatalf("TestQueueCloseUnblocks(%s): fill Push got (ok=%v err=%v), want (true,nil)", m.name, ok, err)
			}
			errCh := make(chan error, 1)
			started := make(chan struct{})
			go func() {
				close(started) // about to enter Push
				_, err := q.Push(ctx, []Number[int]{m.item(1)})
				errCh <- err
			}()
			<-started
			time.Sleep(50 * time.Millisecond)
			if err := q.Close(ctx); err != nil {
				t.Fatalf("TestQueueCloseUnblocks(%s): Close got err == %s, want err == nil", m.name, err)
			}
			select {
			case err := <-errCh:
				if !errors.Is(err, ErrClosed) {
					t.Errorf("TestQueueCloseUnblocks(%s): blocked Push after Close got err == %v, want ErrClosed", m.name, err)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("TestQueueCloseUnblocks(%s): Push did not unblock after Close", m.name)
			}
		}()
	}
}

// closedOrCauser is the per-backing helper under test. Every backing implements it.
type closedOrCauser interface {
	closedOrCause(ctx context.Context) error
}

// TestClosedOrCausePrecedence checks the ctx.Done() arm helper directly (no timing):
// a closed backing yields ErrClosed even when ctx is canceled (Close precedence); an
// open backing yields the ctx cause. Covers an in-memory and the on-disk backing.
func TestClosedOrCausePrecedence(t *testing.T) {
	tests := []struct {
		name string
		make func(t *testing.T, ctx context.Context) Backing[Number[int]]
	}{
		{
			name: "fifo",
			make: func(t *testing.T, ctx context.Context) Backing[Number[int]] {
				b, err := NewFIFO[Number[int]]()
				if err != nil {
					t.Fatalf("TestClosedOrCausePrecedence(fifo): NewFIFO err == %s", err)
				}
				return b
			},
		},
		{
			name: "bbolt",
			make: func(t *testing.T, ctx context.Context) Backing[Number[int]] {
				b, err := NewBboltFIFO[Number[int]](ctx, diskRoot(t))
				if err != nil {
					t.Fatalf("TestClosedOrCausePrecedence(bbolt): NewBboltFIFO err == %s", err)
				}
				return b
			},
		},
	}
	for _, test := range tests {
		ctx := t.Context()
		b := test.make(t, ctx)
		c, ok := b.(closedOrCauser)
		if !ok {
			t.Fatalf("TestClosedOrCausePrecedence(%s): backing does not implement closedOrCause", test.name)
		}

		cctx, cancel := context.WithCancel(ctx)
		cancel()

		// Open backing + canceled ctx: returns the ctx cause, not ErrClosed.
		if err := c.closedOrCause(cctx); !errors.Is(err, context.Canceled) || errors.Is(err, ErrClosed) {
			t.Errorf("TestClosedOrCausePrecedence(%s): open got err == %v, want context.Canceled", test.name, err)
		}

		// Closed backing + canceled ctx: ErrClosed takes precedence.
		if err := b.Close(ctx); err != nil {
			t.Fatalf("TestClosedOrCausePrecedence(%s): Close got err == %s", test.name, err)
		}
		if err := c.closedOrCause(cctx); !errors.Is(err, ErrClosed) {
			t.Errorf("TestClosedOrCausePrecedence(%s): closed got err == %v, want ErrClosed (Close precedence)", test.name, err)
		}
	}
}
