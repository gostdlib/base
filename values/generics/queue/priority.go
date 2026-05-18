package queue

import (
	"container/heap"
	"context"
	"errors"
	"iter"
	"sort"
)

// minHeap implements container/heap.Interface ordered by prioritySeqLess (defined in btree.go).
type minHeap[T Item[T]] struct {
	items []seqItem[T]
}

func (h *minHeap[T]) Len() int           { return len(h.items) }
func (h *minHeap[T]) Less(i, j int) bool { return prioritySeqLess(h.items[i], h.items[j]) }
func (h *minHeap[T]) Swap(i, j int)      { h.items[i], h.items[j] = h.items[j], h.items[i] }

func (h *minHeap[T]) Push(x any) {
	h.items = append(h.items, x.(seqItem[T]))
}

func (h *minHeap[T]) Pop() any {
	n := len(h.items)
	x := h.items[n-1]
	var zero seqItem[T]
	h.items[n-1] = zero
	h.items = h.items[:n-1]
	return x
}

// priorityHeap is an in-memory priority queue backed by container/heap. For unbounded or
// very large priority queues prefer NewBTreePriority, whose tree avoids the heap's
// reheapify cost on Del.
//
// Items pop in Item.Less order; ties break by insert sequence.
// Semantics (blocking, hydration, backup mirror) mirror fifo. lk and maxSize are injected
// by New via setQueueLock and setMaxSize before any other use.
type priorityHeap[T Item[T]] struct {
	lk         *qlock
	h          minHeap[T]
	nextSeq    uint64
	maxSize    int
	notFullCh  chan struct{}
	notEmptyCh chan struct{}
	closed     bool
	backup     Backup[T]
}

// NewPriority returns an in-memory priority Backing backed by container/heap. Items pop in
// Item.Less order, ties broken by insert order. Pass the result to New.
func NewPriority[T Item[T]]() (Backing[T], error) {
	return &priorityHeap[T]{
		lk:         &qlock{},
		notFullCh:  make(chan struct{}),
		notEmptyCh: make(chan struct{}),
	}, nil
}

func (p *priorityHeap[T]) private() {}

func (p *priorityHeap[T]) setQueueLock(lk *qlock) { p.lk = lk }

func (p *priorityHeap[T]) setMaxBatch(n int) error {
	if n < 1 {
		return errors.New("max batch must be at least 1")
	}
	return nil
}

func (p *priorityHeap[T]) setMaxSize(n int) error {
	if n < 0 {
		return errors.New("max size must be at least 0")
	}
	p.maxSize = n
	return nil
}

// Hydrate implements Backing.Hydrate().
func (p *priorityHeap[T]) Hydrate(ctx context.Context, b Backup[T]) error {
	p.lk.lock()
	defer p.lk.unlock()
	for v, err := range b.RangeAll(ctx) {
		if err != nil {
			return err
		}
		if err := validateKindOne(true, v); err != nil {
			return err
		}
		if err := b.OnLoad(ctx, v); err != nil {
			return err
		}
		heap.Push(&p.h, seqItem[T]{seq: p.nextSeq, item: v})
		p.nextSeq++
	}
	p.backup = b
	return nil
}

// Push implements Backing.Push().
func (p *priorityHeap[T]) Push(ctx context.Context, vs []T) error {
	if err := validateKind(true, vs); err != nil {
		return err
	}
	for {
		p.lk.lock()
		if p.closed {
			p.lk.unlock()
			return ErrClosed
		}
		if p.maxSize > 0 && len(vs) > p.maxSize {
			p.lk.unlock()
			return ErrBatchTooLarge
		}
		if p.maxSize == 0 || p.h.Len()+len(vs) <= p.maxSize {
			if p.backup != nil {
				if err := p.backup.Push(ctx, vs); err != nil {
					p.lk.unlock()
					return err
				}
			}
			wasEmpty := p.h.Len() == 0
			for _, v := range vs {
				heap.Push(&p.h, seqItem[T]{seq: p.nextSeq, item: v})
				p.nextSeq++
			}
			if wasEmpty {
				resetSignal(&p.notEmptyCh)
			}
			p.lk.unlock()
			return nil
		}
		wait := p.notFullCh
		p.lk.unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

// PopN implements Backing.PopN().
func (p *priorityHeap[T]) PopN(ctx context.Context, n int) ([]T, error) {
	for {
		p.lk.lock()
		if p.h.Len() > 0 {
			k := n
			if k > p.h.Len() {
				k = p.h.Len()
			}
			wasFull := p.maxSize > 0 && p.h.Len() == p.maxSize
			popped := make([]seqItem[T], 0, k)
			out := make([]T, 0, k)
			for i := 0; i < k; i++ {
				x := heap.Pop(&p.h).(seqItem[T])
				popped = append(popped, x)
				out = append(out, x.item)
			}
			// Mirror the exact popped items to the backup; on failure restore the
			// heap and report nothing removed.
			if p.backup != nil {
				if err := p.backup.Del(ctx, out); err != nil {
					for _, x := range popped {
						heap.Push(&p.h, x)
					}
					p.lk.unlock()
					return nil, err
				}
			}
			if wasFull {
				resetSignal(&p.notFullCh)
			}
			p.lk.unlock()
			return out, nil
		}
		if p.closed {
			p.lk.unlock()
			return nil, ErrClosed
		}
		wait := p.notEmptyCh
		p.lk.unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
	}
}

// Peek implements Backing.Peek().
func (p *priorityHeap[T]) Peek(ctx context.Context) (T, bool, error) {
	var zero T
	p.lk.rlock()
	defer p.lk.runlock()
	if p.closed {
		return zero, false, ErrClosed
	}
	if p.h.Len() == 0 {
		return zero, false, nil
	}
	return p.h.items[0].item, true, nil
}

// Exists implements Backing.Exists().
func (p *priorityHeap[T]) Exists(ctx context.Context, v T) (bool, error) {
	p.lk.rlock()
	defer p.lk.runlock()
	if p.closed {
		return false, ErrClosed
	}
	for _, it := range p.h.items {
		if it.item.Equal(v) {
			return true, nil
		}
	}
	return false, nil
}

// Del implements Backing.Del().
func (p *priorityHeap[T]) Del(ctx context.Context, v []T) error {
	p.lk.lock()
	defer p.lk.unlock()
	if p.closed {
		return ErrClosed
	}
	wasFull := p.maxSize > 0 && p.h.Len() >= p.maxSize
	kept := make([]seqItem[T], 0, len(p.h.items))
	var removed []T
	for _, it := range p.h.items {
		if matchesAny(it.item, v) {
			removed = append(removed, it.item)
			continue
		}
		kept = append(kept, it)
	}
	if len(removed) == 0 {
		return nil
	}
	if p.backup != nil {
		if err := p.backup.Del(ctx, removed); err != nil {
			return err
		}
	}
	p.h.items = kept
	heap.Init(&p.h)
	if wasFull && (p.maxSize == 0 || p.h.Len() < p.maxSize) {
		resetSignal(&p.notFullCh)
	}
	return nil
}

// NotEmpty implements Backing.NotEmpty().
func (p *priorityHeap[T]) NotEmpty(ctx context.Context) error {
	for {
		p.lk.rlock()
		if p.closed {
			p.lk.runlock()
			return ErrClosed
		}
		if p.h.Len() > 0 {
			p.lk.runlock()
			return nil
		}
		wait := p.notEmptyCh
		p.lk.runlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

// NotFull implements Backing.NotFull().
func (p *priorityHeap[T]) NotFull(ctx context.Context) error {
	for {
		p.lk.rlock()
		if p.closed {
			p.lk.runlock()
			return ErrClosed
		}
		if p.maxSize == 0 || p.h.Len() < p.maxSize {
			p.lk.runlock()
			return nil
		}
		wait := p.notFullCh
		p.lk.runlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

// Len implements Backing.Len().
func (p *priorityHeap[T]) Len() int64 {
	p.lk.rlock()
	defer p.lk.runlock()
	return int64(p.h.Len())
}

// Close implements Backing.Close().
func (p *priorityHeap[T]) Close(ctx context.Context) error {
	p.lk.lock()
	defer p.lk.unlock()
	if p.closed {
		return nil
	}
	var err error
	if p.backup != nil {
		err = p.backup.Close(ctx)
	}
	p.closed = true
	close(p.notEmptyCh)
	close(p.notFullCh)
	return err
}

// Clear implements Backing.Clear().
func (p *priorityHeap[T]) Clear(ctx context.Context) error {
	p.lk.lock()
	defer p.lk.unlock()
	if p.closed {
		return ErrClosed
	}
	if p.backup != nil {
		if err := p.backup.Clear(ctx); err != nil {
			return err
		}
	}
	wasFull := p.maxSize > 0 && p.h.Len() >= p.maxSize
	var zero seqItem[T]
	for i := range p.h.items {
		p.h.items[i] = zero
	}
	p.h.items = p.h.items[:0]
	if wasFull {
		resetSignal(&p.notFullCh)
	}
	return nil
}

func (p *priorityHeap[T]) sortedSnapshot() ([]seqItem[T], bool) {
	if p.closed {
		return nil, false
	}
	items := make([]seqItem[T], len(p.h.items))
	copy(items, p.h.items)
	sort.SliceStable(items, func(i, j int) bool {
		return prioritySeqLess(items[i], items[j])
	})
	return items, true
}

// All implements Backing.All(). Unlike the other backings it takes the write lock for
// the whole iteration rather than the read lock: yielding in priority order requires the
// heap's backing array to be sorted, and sorting it ascending in place leaves a valid
// min-heap (every descendant index sorts at or after its ancestor), so Push/Pop keep
// working without a snapshot copy. AllCOW is the read-lock, snapshot variant.
func (p *priorityHeap[T]) All(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		p.lk.lock() // This must be a lock not a rlock because we have to sort.
		defer p.lk.unlock()
		var zero T
		if p.closed {
			yield(zero, ErrClosed)
			return
		}
		sort.SliceStable(p.h.items, func(i, j int) bool {
			return prioritySeqLess(p.h.items[i], p.h.items[j])
		})
		for i := range p.h.items {
			select {
			case <-ctx.Done():
				yield(zero, context.Cause(ctx))
				return
			default:
			}
			if !yield(p.h.items[i].item, nil) {
				return
			}
		}
	}
}

// AllCOW implements Backing.AllCOW().
func (p *priorityHeap[T]) AllCOW(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		p.lk.rlock()
		items, ok := p.sortedSnapshot()
		p.lk.runlock()
		if !ok {
			yield(zero, ErrClosed)
			return
		}
		for _, it := range items {
			select {
			case <-ctx.Done():
				yield(zero, context.Cause(ctx))
				return
			default:
			}
			if !yield(it.item, nil) {
				return
			}
		}
	}
}
