package queue

import (
	"errors"
	"testing"
)

// TestPopAfterCloseNonEmpty is a regression test for a double-close panic: every
// backing's Pop checked closed only after the non-empty branch, so Pop on a closed but
// still-populated bounded queue took the items branch and called resetSignal, which
// close()s notFullCh — already closed by Close — panicking. Closed Pop must instead
// return ErrClosed (consistent with Push/Del/Clear) with no panic, bounded or not.
func TestPopAfterCloseNonEmpty(t *testing.T) {
	for _, m := range queueMakers() {
		for _, maxSize := range []int{0, 2} {
			func() {
				ctx := t.Context()
				q := m.make(t, ctx, maxSize)
				if ok, err := q.Push(ctx, []Number[int]{m.item(1), m.item(2)}); err != nil || !ok {
					t.Fatalf("TestPopAfterCloseNonEmpty(%s,max=%d): fill Push got (ok=%v err=%v), want (true,nil)", m.name, maxSize, ok, err)
				}
				if err := q.Close(ctx); err != nil {
					t.Fatalf("TestPopAfterCloseNonEmpty(%s,max=%d): Close got err == %s, want err == nil", m.name, maxSize, err)
				}
				items, err := q.Pop(ctx, 1)
				switch {
				case err == nil:
					t.Errorf("TestPopAfterCloseNonEmpty(%s,max=%d): Pop after Close got (items=%v, nil), want ErrClosed", m.name, maxSize, items)
				case !errors.Is(err, ErrClosed):
					t.Errorf("TestPopAfterCloseNonEmpty(%s,max=%d): Pop after Close got err == %v, want ErrClosed", m.name, maxSize, err)
				}
			}()
		}
	}
}
