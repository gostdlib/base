package queue

import (
	"context"
	"errors"
	"iter"
)

// btypeFIFO is an in-memory FIFO queue backed by the positional copy-on-write B-tree in
// btype_btree.go (PushBack/PopFront), which avoids the comparator descent and seqItem
// wrapper of the tidwall/btree-based FIFO. Semantics (blocking, hydration, backup mirror)
// mirror fifo. lk and maxSize are injected by New via setQueueLock and setMaxSize before
// any other use.
type btypeFIFO[T Item[T]] struct {
	lk         *qlock
	t          tree[omit, T]
	maxSize    int
	notFullCh  chan struct{}
	notEmptyCh chan struct{}
	closed     bool
	backup     Backup[T]
}

// newBtypeFIFO returns an in-memory FIFO Backing backed by the positional B-tree. It is the
// backing NewBTreeFIFO uses when WithIndex is not set: unbounded-friendly with cheap
// push/pop and O(n) Exists/Del.
func newBtypeFIFO[T Item[T]]() (Backing[T], error) {
	return &btypeFIFO[T]{
		lk:         &qlock{},
		notFullCh:  make(chan struct{}),
		notEmptyCh: make(chan struct{}),
	}, nil
}

func (f *btypeFIFO[T]) private() {}

func (f *btypeFIFO[T]) setQueueLock(lk *qlock) { f.lk = lk }

func (f *btypeFIFO[T]) setMaxBatch(n int) error {
	if n < 1 {
		return errors.New("max batch must be at least 1")
	}
	return nil
}

func (f *btypeFIFO[T]) setMaxSize(n int) error {
	if n < 0 {
		return errors.New("max size must be at least 0")
	}
	f.maxSize = n
	return nil
}

// Hydrate implements Backing.Hydrate().
func (f *btypeFIFO[T]) Hydrate(ctx context.Context, b Backup[T]) error {
	f.lk.lock()
	defer f.lk.unlock()
	for v, err := range b.RangeAll(ctx) {
		if err != nil {
			return err
		}
		if err := validateKindOne(false, v); err != nil {
			return err
		}
		if err := b.OnLoad(ctx, v); err != nil {
			return err
		}
		f.t.PushBack(omit{}, v)
	}
	f.backup = b
	return nil
}

// Push implements Backing.Push().
func (f *btypeFIFO[T]) Push(ctx context.Context, vs []T) error {
	if err := validateKind(false, vs); err != nil {
		return err
	}
	for {
		f.lk.lock()
		if f.closed {
			f.lk.unlock()
			return ErrClosed
		}
		if f.maxSize > 0 && len(vs) > f.maxSize {
			f.lk.unlock()
			return ErrBatchTooLarge
		}
		if f.maxSize == 0 || f.t.Len()+len(vs) <= f.maxSize {
			if f.backup != nil {
				if err := f.backup.Push(ctx, vs); err != nil {
					f.lk.unlock()
					return err
				}
			}
			wasEmpty := f.t.Len() == 0
			for _, v := range vs {
				f.t.PushBack(omit{}, v)
			}
			if wasEmpty {
				resetSignal(&f.notEmptyCh)
			}
			f.lk.unlock()
			return nil
		}
		wait := f.notFullCh
		f.lk.unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return f.closedOrCause(ctx)
		}
	}
}

// Pop implements Backing.Pop().
func (f *btypeFIFO[T]) Pop(ctx context.Context, n int) ([]T, error) {
	for {
		f.lk.lock()
		if f.t.Len() > 0 {
			k := n
			if k > f.t.Len() {
				k = f.t.Len()
			}
			wasFull := f.maxSize > 0 && f.t.Len() == f.maxSize
			out := make([]T, 0, k)
			if f.backup != nil {
				// Peek the first k in order without mutating, so the backup is
				// mirrored before anything is removed.
				for _, v := range f.t.All() {
					out = append(out, v)
					if len(out) == k {
						break
					}
				}
				if err := f.backup.Del(ctx, out); err != nil {
					f.lk.unlock()
					return nil, err
				}
				for i := 0; i < k; i++ {
					f.t.PopFront()
				}
			} else {
				for i := 0; i < k; i++ {
					_, v, _ := f.t.PopFront()
					out = append(out, v)
				}
			}
			if wasFull {
				resetSignal(&f.notFullCh)
			}
			f.lk.unlock()
			return out, nil
		}
		if f.closed {
			f.lk.unlock()
			return nil, ErrClosed
		}
		wait := f.notEmptyCh
		f.lk.unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, f.closedOrCause(ctx)
		}
	}
}

// Peek implements Backing.Peek().
func (f *btypeFIFO[T]) Peek(ctx context.Context) (T, bool, error) {
	var zero T
	f.lk.rlock()
	defer f.lk.runlock()
	if f.closed {
		return zero, false, ErrClosed
	}
	if f.t.Len() == 0 {
		return zero, false, nil
	}
	_, v, _ := f.t.Front()
	return v, true, nil
}

// Exists implements Backing.Exists().
func (f *btypeFIFO[T]) Exists(ctx context.Context, v T) (bool, error) {
	f.lk.rlock()
	defer f.lk.runlock()
	if f.closed {
		return false, ErrClosed
	}
	for _, it := range f.t.All() {
		if it.Equal(v) {
			return true, nil
		}
	}
	return false, nil
}

// Del implements Backing.Del().
func (f *btypeFIFO[T]) Del(ctx context.Context, v []T) error {
	f.lk.lock()
	defer f.lk.unlock()
	if f.closed {
		return ErrClosed
	}
	wasFull := f.maxSize > 0 && f.t.Len() >= f.maxSize
	var rmIdx []int
	var removed []T
	i := 0
	for _, it := range f.t.All() {
		if matchesAny(it, v) {
			rmIdx = append(rmIdx, i)
			removed = append(removed, it)
		}
		i++
	}
	if len(removed) == 0 {
		return nil
	}
	if f.backup != nil {
		if err := f.backup.Del(ctx, removed); err != nil {
			return err
		}
	}
	// Delete from highest index down so earlier indices stay valid.
	for j := len(rmIdx) - 1; j >= 0; j-- {
		f.t.DeleteAt(rmIdx[j])
	}
	if wasFull && (f.maxSize == 0 || f.t.Len() < f.maxSize) {
		resetSignal(&f.notFullCh)
	}
	return nil
}

// NotEmpty implements Backing.NotEmpty().
func (f *btypeFIFO[T]) NotEmpty(ctx context.Context) error {
	for {
		f.lk.rlock()
		if f.closed {
			f.lk.runlock()
			return ErrClosed
		}
		if f.t.Len() > 0 {
			f.lk.runlock()
			return nil
		}
		wait := f.notEmptyCh
		f.lk.runlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return f.closedOrCause(ctx)
		}
	}
}

// NotFull implements Backing.NotFull().
func (f *btypeFIFO[T]) NotFull(ctx context.Context) error {
	for {
		f.lk.rlock()
		if f.closed {
			f.lk.runlock()
			return ErrClosed
		}
		if f.maxSize == 0 || f.t.Len() < f.maxSize {
			f.lk.runlock()
			return nil
		}
		wait := f.notFullCh
		f.lk.runlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return f.closedOrCause(ctx)
		}
	}
}

// Len implements Backing.Len().
func (f *btypeFIFO[T]) Len() int64 {
	f.lk.rlock()
	defer f.lk.runlock()
	return int64(f.t.Len())
}

// closedOrCause returns ErrClosed if the backing has been closed, else the ctx cause.
// Used in the ctx.Done() arm of a blocked wait so Close deterministically wins a race
// with ctx cancellation.
func (f *btypeFIFO[T]) closedOrCause(ctx context.Context) error {
	f.lk.lock()
	c := f.closed
	f.lk.unlock()
	if c {
		return ErrClosed
	}
	cause := context.Cause(ctx)
	return cause
}

// Close implements Backing.Close().
func (f *btypeFIFO[T]) Close(ctx context.Context) error {
	f.lk.lock()
	defer f.lk.unlock()
	if f.closed {
		return nil
	}
	var err error
	if f.backup != nil {
		err = f.backup.Close(ctx)
	}
	f.closed = true
	close(f.notEmptyCh)
	close(f.notFullCh)
	return err
}

// Clear implements Backing.Clear().
func (f *btypeFIFO[T]) Clear(ctx context.Context) error {
	f.lk.lock()
	defer f.lk.unlock()
	if f.closed {
		return ErrClosed
	}
	if f.backup != nil {
		if err := f.backup.Clear(ctx); err != nil {
			return err
		}
	}
	wasFull := f.maxSize > 0 && f.t.Len() >= f.maxSize
	f.t.Clear()
	if wasFull {
		resetSignal(&f.notFullCh)
	}
	return nil
}

// All implements Backing.All().
func (f *btypeFIFO[T]) All(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		f.lk.rlock()
		defer f.lk.runlock()
		if f.closed {
			var zero T
			yield(zero, ErrClosed)
			return
		}
		for _, v := range f.t.All() {
			select {
			case <-ctx.Done():
				var zero T
				yield(zero, context.Cause(ctx))
				return
			default:
			}
			if !yield(v, nil) {
				return
			}
		}
	}
}

// AllCOW implements Backing.AllCOW().
func (f *btypeFIFO[T]) AllCOW(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		// Take the write lock only for the O(1) copy-on-write snapshot: CopyInto
		// flips the source's copied flag (a non-atomic field), so it must be
		// exclusive of readers and writers. The lock is released before any yield,
		// so writers never block during iteration and the loop may safely call
		// mutating Queue methods.
		f.lk.lock()
		if f.closed {
			f.lk.unlock()
			yield(zero, ErrClosed)
			return
		}
		var snap tree[omit, T]
		f.t.CopyInto(&snap)
		f.lk.unlock()
		defer snap.Release()

		for _, v := range snap.All() {
			select {
			case <-ctx.Done():
				yield(zero, context.Cause(ctx))
				return
			default:
			}
			if !yield(v, nil) {
				return
			}
		}
	}
}
