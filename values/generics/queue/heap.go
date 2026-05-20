package queue

import (
	"container/heap"
	"context"
	"errors"
	"iter"
	"sort"
	"sync/atomic"
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
	lk              *qlock
	h               minHeap[T]
	nextSeq         uint64
	maxSize         int
	notFullCh       chan struct{}
	notEmptyCh      chan struct{}
	notFullWaiters  atomic.Int32
	notEmptyWaiters atomic.Int32
	closed          bool
	backup          Backup[T]
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
	// Append in backup order, then heapify once: heap.Init is O(n) (Floyd build-heap)
	// vs O(n log n) for a per-item heap.Push. seq is assigned in RangeAll order so the
	// pop order (prioritySeqLess) is unchanged regardless of the post-Init slice layout.
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
		p.h.items = append(p.h.items, seqItem[T]{seq: p.nextSeq, item: v})
		p.nextSeq++
	}
	heap.Init(&p.h)
	p.backup = b
	return nil
}

// Push implements Backing.Push().
func (p *priorityHeap[T]) Push(ctx context.Context, vs []T) error {
	if err := validateKind(true, vs); err != nil {
		return err
	}
	parked := false
	for {
		p.lk.lock()
		if parked {
			p.notFullWaiters.Add(-1)
			parked = false
		}
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
			if wasEmpty && p.notEmptyWaiters.Load() > 0 {
				resetSignal(&p.notEmptyCh)
			}
			p.lk.unlock()
			return nil
		}
		wait := p.notFullCh
		p.notFullWaiters.Add(1)
		parked = true
		p.lk.unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			p.notFullWaiters.Add(-1)
			return p.closedOrCause(ctx)
		}
	}
}

// Pop implements Backing.Pop().
func (p *priorityHeap[T]) Pop(ctx context.Context, n int) ([]T, error) {
	parked := false
	for {
		p.lk.lock()
		if parked {
			p.notEmptyWaiters.Add(-1)
			parked = false
		}
		if p.closed {
			p.lk.unlock()
			return nil, ErrClosed
		}
		if p.h.Len() > 0 {
			k := n
			if k > p.h.Len() {
				k = p.h.Len()
			}
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
			// Freed capacity: wake any parked producer. Gated on notFullWaiters
			// so the steady-state case (no producer waiting) does no chan alloc.
			if p.notFullWaiters.Load() > 0 {
				resetSignal(&p.notFullCh)
			}
			p.lk.unlock()
			return out, nil
		}
		wait := p.notEmptyCh
		p.notEmptyWaiters.Add(1)
		parked = true
		p.lk.unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			p.notEmptyWaiters.Add(-1)
			return nil, p.closedOrCause(ctx)
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
	// Freed capacity: gated on notFullWaiters; see Pop.
	if p.notFullWaiters.Load() > 0 {
		resetSignal(&p.notFullCh)
	}
	return nil
}

// NotEmpty implements Backing.NotEmpty().
func (p *priorityHeap[T]) NotEmpty(ctx context.Context) error {
	parked := false
	for {
		p.lk.rlock()
		if parked {
			p.notEmptyWaiters.Add(-1)
			parked = false
		}
		if p.closed {
			p.lk.runlock()
			return ErrClosed
		}
		if p.h.Len() > 0 {
			p.lk.runlock()
			return nil
		}
		wait := p.notEmptyCh
		p.notEmptyWaiters.Add(1)
		parked = true
		p.lk.runlock()
		select {
		case <-wait:
		case <-ctx.Done():
			p.notEmptyWaiters.Add(-1)
			return p.closedOrCause(ctx)
		}
	}
}

// NotFull implements Backing.NotFull().
func (p *priorityHeap[T]) NotFull(ctx context.Context) error {
	parked := false
	for {
		p.lk.rlock()
		if parked {
			p.notFullWaiters.Add(-1)
			parked = false
		}
		if p.closed {
			p.lk.runlock()
			return ErrClosed
		}
		if p.maxSize == 0 || p.h.Len() < p.maxSize {
			p.lk.runlock()
			return nil
		}
		wait := p.notFullCh
		p.notFullWaiters.Add(1)
		parked = true
		p.lk.runlock()
		select {
		case <-wait:
		case <-ctx.Done():
			p.notFullWaiters.Add(-1)
			return p.closedOrCause(ctx)
		}
	}
}

// Len implements Backing.Len().
func (p *priorityHeap[T]) Len() int64 {
	p.lk.rlock()
	defer p.lk.runlock()
	return int64(p.h.Len())
}

// closedOrCause returns ErrClosed if the backing has been closed, else the ctx cause.
// Used in the ctx.Done() arm of a blocked wait so Close deterministically wins a race
// with ctx cancellation.
func (p *priorityHeap[T]) closedOrCause(ctx context.Context) error {
	p.lk.rlock()
	c := p.closed
	p.lk.runlock()
	if c {
		return ErrClosed
	}
	cause := context.Cause(ctx)
	return cause
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
	if p.h.Len() == 0 {
		return nil
	}
	if p.backup != nil {
		if err := p.backup.Clear(ctx); err != nil {
			return err
		}
	}
	var zero seqItem[T]
	for i := range p.h.items {
		p.h.items[i] = zero
	}
	p.h.items = p.h.items[:0]
	// Freed capacity: gated on notFullWaiters; see Pop.
	if p.notFullWaiters.Load() > 0 {
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

// All implements Backing.All(). Unlike the other backings it holds the write lock for the
// whole iteration, not the read lock: yielding in priority order requires sorting, and
// sorting the heap array ascending in place leaves a valid min-heap (a node's children are
// always at higher indices), so Push/Pop keep working with no snapshot copy. AllCOW is the
// read-lock, snapshot variant.
func (p *priorityHeap[T]) All(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		p.lk.lock()
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
