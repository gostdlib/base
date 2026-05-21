package queue

import (
	"context"
	"errors"
	"iter"
)

// fifo is an in-memory FIFO queue backed by a slice.
//
// notFull and notEmpty are signal primitives; mutators call Signal() only when
// HasWaiters() reports a parked waiter, so the steady-state case (no waiters)
// is allocation-free.
type fifo[T Item[T]] struct {
	lk       *qlock
	items    []T
	maxSize  int
	notFull  *signal
	notEmpty *signal
	closed   bool
	backup   Backup[T]
}

// NewFIFO returns an in-memory FIFO Backing backed by a slice. Use this when queue size is
// going to be < 10K items.
func NewFIFO[T Item[T]]() (Backing[T], error) {
	return &fifo[T]{
		lk:       &qlock{},
		notFull:  newSignal(),
		notEmpty: newSignal(),
	}, nil
}

func (f *fifo[T]) private() {}

func (f *fifo[T]) setQueueLock(lk *qlock) { f.lk = lk }

func (f *fifo[T]) setMaxBatch(n int) error {
	if n < 1 {
		return errors.New("max batch must be at least 1")
	}
	return nil
}

func (f *fifo[T]) setMaxSize(n int) error {
	if n < 0 {
		return errors.New("max size must be at least 0")
	}
	f.maxSize = n
	return nil
}

// Hydrate implements Backing.Hydrate().
func (f *fifo[T]) Hydrate(ctx context.Context, b Backup[T]) error {
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
		f.items = append(f.items, v)
	}
	f.backup = b
	return nil
}

// Push implements Backing.Push().
func (f *fifo[T]) Push(ctx context.Context, vs []T) error {
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
		if f.maxSize == 0 || len(f.items)+len(vs) <= f.maxSize {
			if f.backup != nil {
				if err := f.backup.Push(ctx, vs); err != nil {
					f.lk.unlock()
					return err
				}
			}
			wasEmpty := len(f.items) == 0
			f.items = append(f.items, vs...)
			if wasEmpty && f.notEmpty.HasWaiters() {
				f.notEmpty.Signal()
			}
			f.lk.unlock()
			return nil
		}
		// f.lk is released by Wait, but only AFTER it has synchronously
		// registered as a waiter (Mesa invariant).
		if err := f.notFull.Wait(ctx, f.lk.unlock); err != nil {
			return f.closedOrCause(ctx)
		}
	}
}

// Pop implements Backing.Pop().
func (f *fifo[T]) Pop(ctx context.Context, n int) ([]T, error) {
	var zero T
	for {
		f.lk.lock()
		if f.closed {
			f.lk.unlock()
			return nil, ErrClosed
		}
		if len(f.items) > 0 {
			k := n
			if k > len(f.items) {
				k = len(f.items)
			}
			out := make([]T, k)
			copy(out, f.items[:k])
			// Mirror the exact popped items to the backup before removing them from
			// the queue, so a backup failure aborts with nothing removed.
			if f.backup != nil {
				if err := f.backup.Del(ctx, out); err != nil {
					f.lk.unlock()
					return nil, err
				}
			}
			for i := 0; i < k; i++ {
				f.items[i] = zero
			}
			f.items = f.items[k:]
			// Freed capacity: wake any parked producer. Gated on HasWaiters so
			// the steady-state case (no producer waiting) skips the Signal.
			if f.notFull.HasWaiters() {
				f.notFull.Signal()
			}
			f.lk.unlock()
			return out, nil
		}
		if err := f.notEmpty.Wait(ctx, f.lk.unlock); err != nil {
			return nil, f.closedOrCause(ctx)
		}
	}
}

// Peek implements Backing.Peek().
func (f *fifo[T]) Peek(ctx context.Context) (T, bool, error) {
	var zero T
	f.lk.rlock()
	defer f.lk.runlock()
	if f.closed {
		return zero, false, ErrClosed
	}
	if len(f.items) == 0 {
		return zero, false, nil
	}
	return f.items[0], true, nil
}

// Exists implements Backing.Exists().
func (f *fifo[T]) Exists(ctx context.Context, v T) (bool, error) {
	f.lk.rlock()
	defer f.lk.runlock()
	if f.closed {
		return false, ErrClosed
	}
	for i := range f.items {
		if f.items[i].Equal(v) {
			return true, nil
		}
	}
	return false, nil
}

// Del implements Backing.Del().
func (f *fifo[T]) Del(ctx context.Context, v []T) error {
	f.lk.lock()
	defer f.lk.unlock()
	if f.closed {
		return ErrClosed
	}
	kept := make([]T, 0, len(f.items))
	var removed []T
	for _, item := range f.items {
		if matchesAny(item, v) {
			removed = append(removed, item)
			continue
		}
		kept = append(kept, item)
	}
	if len(removed) == 0 {
		return nil
	}
	// Mirror the exact removed items to the backup before applying the deletion.
	if f.backup != nil {
		if err := f.backup.Del(ctx, removed); err != nil {
			return err
		}
	}
	f.items = kept
	// Freed capacity: gated on HasWaiters; see Pop.
	if f.notFull.HasWaiters() {
		f.notFull.Signal()
	}
	return nil
}

// NotEmpty implements Backing.NotEmpty().
func (f *fifo[T]) NotEmpty(ctx context.Context) error {
	for {
		f.lk.rlock()
		if f.closed {
			f.lk.runlock()
			return ErrClosed
		}
		if len(f.items) > 0 {
			f.lk.runlock()
			return nil
		}
		if err := f.notEmpty.Wait(ctx, f.lk.runlock); err != nil {
			return f.closedOrCause(ctx)
		}
	}
}

// NotFull implements Backing.NotFull().
func (f *fifo[T]) NotFull(ctx context.Context) error {
	for {
		f.lk.rlock()
		if f.closed {
			f.lk.runlock()
			return ErrClosed
		}
		if f.maxSize == 0 || len(f.items) < f.maxSize {
			f.lk.runlock()
			return nil
		}
		if err := f.notFull.Wait(ctx, f.lk.runlock); err != nil {
			return f.closedOrCause(ctx)
		}
	}
}

// Len implements Backing.Len().
func (f *fifo[T]) Len() int64 {
	f.lk.rlock()
	defer f.lk.runlock()
	return int64(len(f.items))
}

// closedOrCause returns ErrClosed if the backing has been closed, else the ctx cause.
// Used in the ctx.Done() arm of a blocked wait so Close deterministically wins a race
// with ctx cancellation.
func (f *fifo[T]) closedOrCause(ctx context.Context) error {
	f.lk.rlock()
	c := f.closed
	f.lk.runlock()
	if c {
		return ErrClosed
	}
	cause := context.Cause(ctx)
	return cause
}

// Close implements Backing.Close().
func (f *fifo[T]) Close(ctx context.Context) error {
	f.lk.lock()
	if f.closed {
		f.lk.unlock()
		return nil
	}
	var err error
	if f.backup != nil {
		err = f.backup.Close(ctx)
	}
	f.closed = true
	f.lk.unlock()
	// Wake any parked Wait callers; they re-acquire f.lk, see closed, return ErrClosed.
	f.notEmpty.Signal()
	f.notFull.Signal()
	return err
}

// Clear implements Backing.Clear().
func (f *fifo[T]) Clear(ctx context.Context) error {
	f.lk.lock()
	defer f.lk.unlock()
	if f.closed {
		return ErrClosed
	}
	if len(f.items) == 0 {
		return nil
	}
	if f.backup != nil {
		if err := f.backup.Clear(ctx); err != nil {
			return err
		}
	}
	var zero T
	for i := range f.items {
		f.items[i] = zero
	}
	f.items = f.items[:0]
	// Freed capacity: gated on HasWaiters; see Pop.
	if f.notFull.HasWaiters() {
		f.notFull.Signal()
	}
	return nil
}

// All implements Backing.All().
func (f *fifo[T]) All(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		f.lk.rlock()
		defer f.lk.runlock()
		if f.closed {
			var zero T
			yield(zero, ErrClosed)
			return
		}
		for i := range f.items {
			select {
			case <-ctx.Done():
				var zero T
				yield(zero, context.Cause(ctx))
				return
			default:
			}
			if !yield(f.items[i], nil) {
				return
			}
		}
	}
}

// AllCOW implements Backing.AllCOW().
func (f *fifo[T]) AllCOW(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		f.lk.rlock()
		if f.closed {
			f.lk.runlock()
			yield(zero, ErrClosed)
			return
		}
		for i := 0; i < len(f.items); i++ {
			if f.lk.writeWanted() {
				snap := append([]T(nil), f.items[i:]...)
				f.lk.runlock()
				for _, v := range snap {
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
				return
			}
			select {
			case <-ctx.Done():
				f.lk.runlock()
				yield(zero, context.Cause(ctx))
				return
			default:
			}
			if !yield(f.items[i], nil) {
				f.lk.runlock()
				return
			}
		}
		f.lk.runlock()
	}
}
