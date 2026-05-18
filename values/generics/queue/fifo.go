package queue

import (
	"context"
	"errors"
	"iter"
)

// fifo is an in-memory FIFO queue backed by a slice.
type fifo[T Item[T]] struct {
	lk         *qlock
	items      []T
	maxSize    int
	notFullCh  chan struct{}
	notEmptyCh chan struct{}
	closed     bool
	backup     Backup[T]
}

// NewFIFO returns an in-memory FIFO Backing backed by a slice. Use this when queue size is
// going to be < 10K items.
func NewFIFO[T Item[T]]() (Backing[T], error) {
	return &fifo[T]{
		lk:         &qlock{},
		notFullCh:  make(chan struct{}),
		notEmptyCh: make(chan struct{}),
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

// resetSignal closes the channel and replaces it with a fresh one. Caller must hold the write lock.
func resetSignal(ch *chan struct{}) {
	close(*ch)
	*ch = make(chan struct{})
}

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
			return context.Cause(ctx)
		}
	}
}

func (f *fifo[T]) PopN(ctx context.Context, n int) ([]T, error) {
	var zero T
	for {
		f.lk.lock()
		if len(f.items) > 0 {
			k := n
			if k > len(f.items) {
				k = len(f.items)
			}
			wasFull := f.maxSize > 0 && len(f.items) == f.maxSize
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
			return nil, context.Cause(ctx)
		}
	}
}

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

func (f *fifo[T]) Del(ctx context.Context, v []T) error {
	f.lk.lock()
	defer f.lk.unlock()
	if f.closed {
		return ErrClosed
	}
	wasFull := f.maxSize > 0 && len(f.items) >= f.maxSize
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
	if wasFull && (f.maxSize == 0 || len(f.items) < f.maxSize) {
		resetSignal(&f.notFullCh)
	}
	return nil
}

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
		wait := f.notEmptyCh
		f.lk.runlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

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
		wait := f.notFullCh
		f.lk.runlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

func (f *fifo[T]) Len() int64 {
	f.lk.rlock()
	defer f.lk.runlock()
	return int64(len(f.items))
}

func (f *fifo[T]) Close(ctx context.Context) error {
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

func (f *fifo[T]) Clear(ctx context.Context) error {
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
	wasFull := f.maxSize > 0 && len(f.items) >= f.maxSize
	var zero T
	for i := range f.items {
		f.items[i] = zero
	}
	f.items = f.items[:0]
	if wasFull {
		resetSignal(&f.notFullCh)
	}
	return nil
}

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
