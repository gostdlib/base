package queue

import (
	"context"
	"errors"
	"iter"

	"github.com/tidwall/btree"
)

// seqItem pairs an item with a monotonic insert sequence so the btree can hold a stable
// FIFO order independent of T's Less.
type seqItem[T Item[T]] struct {
	seq  uint64
	item T
}

// btreeIndex maps Item.Hash() to the exact seqItem locators stored in the tree, so
// Exists/Del do a bucket lookup + Equal scan instead of a full tree scan. nil when WithIndex
// is not set. The locator (seqItem) is the precise tree key, so tree.Delete is O(log n).
type btreeIndex[T Item[T]] struct {
	m map[uint64][]seqItem[T]
}

func newBtreeIndex[T Item[T]]() *btreeIndex[T] {
	return &btreeIndex[T]{m: map[uint64][]seqItem[T]{}}
}

func (x *btreeIndex[T]) add(si seqItem[T]) {
	h := si.item.Hash()
	x.m[h] = append(x.m[h], si)
}

func (x *btreeIndex[T]) remove(hash uint64, seq uint64) {
	s := x.m[hash]
	for i := range s {
		if s[i].seq == seq {
			s[i] = s[len(s)-1]
			x.m[hash] = s[:len(s)-1]
			break
		}
	}
	if len(x.m[hash]) == 0 {
		delete(x.m, hash)
	}
}

func (x *btreeIndex[T]) bucket(hash uint64) []seqItem[T] {
	return x.m[hash]
}

// btreeBacking is an in-memory queue backed by github.com/tidwall/btree, used for both
// FIFO (keyed by insert sequence) and priority (keyed by Item.Less with insert sequence as
// tiebreak). The variant is selected by the less function passed to the constructor; the
// rest of the implementation is identical. The btree avoids the slice/heap backings'
// shift/reheapify cost, which matters for large or unbounded queues.
//
// Semantics (blocking, hydration, backup mirror) mirror fifo. lk and maxSize are injected
// by New via setQueueLock and setMaxSize before any other use.
type btreeBacking[T Item[T]] struct {
	lk         *qlock
	tree       *btree.BTreeG[seqItem[T]]
	idx        *btreeIndex[T]
	nextSeq    uint64
	maxSize    int
	priority   bool
	notFullCh  chan struct{}
	notEmptyCh chan struct{}
	closed     bool
	backup     Backup[T]
}

// fifoSeqLess orders seqItem values by insert sequence only, giving the btree FIFO order.
func fifoSeqLess[T Item[T]](a, b seqItem[T]) bool {
	return a.seq < b.seq
}

// prioritySeqLess orders seqItem values by Item.Less with insert sequence as a tiebreak.
func prioritySeqLess[T Item[T]](a, b seqItem[T]) bool {
	if a.item.Less(b.item) {
		return true
	}
	if b.item.Less(a.item) {
		return false
	}
	return a.seq < b.seq
}

// NewBTreeFIFO returns an in-memory FIFO Backing. Without WithIndex it uses the positional
// btype tree (cheapest push/pop; Exists/Del are O(n) scans like NewFIFO). With WithIndex it
// uses a tidwall/btree keyed by insert sequence plus a hash index, giving O(log n) Del and
// O(1) Exists — required for delete-heavy workloads that scan RangeAll and Del/Exists each
// matching entry. WithBTreeWidth applies only to the indexed (keyed btree) variant.
func NewBTreeFIFO[T Item[T]](options ...BackingOption) (Backing[T], error) {
	o, err := applyBackingOptions(callBTreeFIFO, options)
	if err != nil {
		return nil, err
	}
	if !o.index {
		return NewBtypeFIFO[T]()
	}
	return newBTreeBacking[T](o, fifoSeqLess[T], false)
}

// NewBTreePriority returns an in-memory priority Backing backed by github.com/tidwall/btree,
// keyed by Item.Less with insert sequence as the tiebreak. It accepts WithBTreeWidth and
// WithIndex. Pass the result to New.
func NewBTreePriority[T Item[T]](options ...BackingOption) (Backing[T], error) {
	o, err := applyBackingOptions(callBTreePriority, options)
	if err != nil {
		return nil, err
	}
	return newBTreeBacking[T](o, prioritySeqLess[T], true)
}

func newBTreeBacking[T Item[T]](o backingOpts, less func(a, b seqItem[T]) bool, priority bool) (Backing[T], error) {
	if o.width == 0 {
		o.width = 32
	}
	if o.width < 2 {
		return nil, errors.New("btree width must be at least 2")
	}
	b := &btreeBacking[T]{
		lk: &qlock{},
		tree: btree.NewBTreeGOptions(less, btree.Options{
			Degree:  o.width,
			NoLocks: true,
		}),
		priority:   priority,
		notFullCh:  make(chan struct{}),
		notEmptyCh: make(chan struct{}),
	}
	if o.index {
		b.idx = newBtreeIndex[T]()
	}
	return b, nil
}

func (b *btreeBacking[T]) private() {}

func (b *btreeBacking[T]) setQueueLock(lk *qlock) { b.lk = lk }

func (b *btreeBacking[T]) setMaxBatch(n int) error {
	if n < 1 {
		return errors.New("max batch must be at least 1")
	}
	return nil
}

func (b *btreeBacking[T]) setMaxSize(n int) error {
	if n < 0 {
		return errors.New("max size must be at least 0")
	}
	b.maxSize = n
	return nil
}

func (b *btreeBacking[T]) Hydrate(ctx context.Context, bu Backup[T]) error {
	b.lk.lock()
	defer b.lk.unlock()
	for v, err := range bu.RangeAll(ctx) {
		if err != nil {
			return err
		}
		if err := validateKindOne(b.priority, v); err != nil {
			return err
		}
		if err := bu.OnLoad(ctx, v); err != nil {
			return err
		}
		si := seqItem[T]{seq: b.nextSeq, item: v}
		// Load is an O(1) append for ascending input; seq is strictly increasing
		// (FIFO) and unique (priority tiebreak), so it is correct and much cheaper
		// than Set's full descent.
		b.tree.Load(si)
		b.nextSeq++
		if b.idx != nil {
			b.idx.add(si)
		}
	}
	b.backup = bu
	return nil
}

func (b *btreeBacking[T]) Push(ctx context.Context, vs []T) error {
	if err := validateKind(b.priority, vs); err != nil {
		return err
	}
	for {
		b.lk.lock()
		if b.closed {
			b.lk.unlock()
			return ErrClosed
		}
		if b.maxSize > 0 && len(vs) > b.maxSize {
			b.lk.unlock()
			return ErrBatchTooLarge
		}
		if b.maxSize == 0 || b.tree.Len()+len(vs) <= b.maxSize {
			if b.backup != nil {
				if err := b.backup.Push(ctx, vs); err != nil {
					b.lk.unlock()
					return err
				}
			}
			wasEmpty := b.tree.Len() == 0
			for _, v := range vs {
				si := seqItem[T]{seq: b.nextSeq, item: v}
				// Load: O(1) append for the strictly-increasing seq key.
				b.tree.Load(si)
				b.nextSeq++
				if b.idx != nil {
					b.idx.add(si)
				}
			}
			if wasEmpty {
				resetSignal(&b.notEmptyCh)
			}
			b.lk.unlock()
			return nil
		}
		wait := b.notFullCh
		b.lk.unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

func (b *btreeBacking[T]) PopN(ctx context.Context, n int) ([]T, error) {
	for {
		b.lk.lock()
		if b.tree.Len() > 0 {
			k := n
			if k > b.tree.Len() {
				k = b.tree.Len()
			}
			wasFull := b.maxSize > 0 && b.tree.Len() == b.maxSize
			out := make([]T, 0, k)
			if b.backup != nil {
				// Peek the first k in pop order without mutating, so the backup is
				// mirrored before anything is removed.
				b.tree.Scan(func(it seqItem[T]) bool {
					out = append(out, it.item)
					return len(out) < k
				})
				if err := b.backup.Del(ctx, out); err != nil {
					b.lk.unlock()
					return nil, err
				}
				for i := 0; i < k; i++ {
					mn, _ := b.tree.PopMin()
					if b.idx != nil {
						b.idx.remove(mn.item.Hash(), mn.seq)
					}
				}
			} else {
				// PopMin removes the leftmost (min) directly — no Delete-by-key
				// search and no peek allocation.
				for i := 0; i < k; i++ {
					mn, _ := b.tree.PopMin()
					out = append(out, mn.item)
					if b.idx != nil {
						b.idx.remove(mn.item.Hash(), mn.seq)
					}
				}
			}
			if wasFull {
				resetSignal(&b.notFullCh)
			}
			b.lk.unlock()
			return out, nil
		}
		if b.closed {
			b.lk.unlock()
			return nil, ErrClosed
		}
		wait := b.notEmptyCh
		b.lk.unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
	}
}

func (b *btreeBacking[T]) Peek(ctx context.Context) (T, bool, error) {
	var zero T
	b.lk.rlock()
	defer b.lk.runlock()
	if b.closed {
		return zero, false, ErrClosed
	}
	min, ok := b.tree.Min()
	if !ok {
		return zero, false, nil
	}
	return min.item, true, nil
}

func (b *btreeBacking[T]) Exists(ctx context.Context, v T) (bool, error) {
	b.lk.rlock()
	defer b.lk.runlock()
	if b.closed {
		return false, ErrClosed
	}
	if b.idx != nil {
		for _, si := range b.idx.bucket(v.Hash()) {
			if si.item.Equal(v) {
				return true, nil
			}
		}
		return false, nil
	}
	found := false
	b.tree.Scan(func(it seqItem[T]) bool {
		if it.item.Equal(v) {
			found = true
			return false
		}
		return true
	})
	return found, nil
}

func (b *btreeBacking[T]) Del(ctx context.Context, v []T) error {
	b.lk.lock()
	defer b.lk.unlock()
	if b.closed {
		return ErrClosed
	}
	wasFull := b.maxSize > 0 && b.tree.Len() >= b.maxSize
	var toDel []seqItem[T]
	if b.idx != nil {
		// Scan only the buckets for the distinct hashes in v; dedup collected items
		// by seq so a slot matched by duplicate/same-hash elements of v is removed once.
		seenHash := make(map[uint64]struct{}, len(v))
		seenSeq := make(map[uint64]struct{})
		for e := range v {
			h := v[e].Hash()
			if _, ok := seenHash[h]; ok {
				continue
			}
			seenHash[h] = struct{}{}
			for _, si := range b.idx.bucket(h) {
				if _, ok := seenSeq[si.seq]; ok {
					continue
				}
				if matchesAny(si.item, v) {
					seenSeq[si.seq] = struct{}{}
					toDel = append(toDel, si)
				}
			}
		}
	} else {
		b.tree.Scan(func(it seqItem[T]) bool {
			if matchesAny(it.item, v) {
				toDel = append(toDel, it)
			}
			return true
		})
	}
	if len(toDel) == 0 {
		return nil
	}
	if b.backup != nil {
		removed := make([]T, 0, len(toDel))
		for _, it := range toDel {
			removed = append(removed, it.item)
		}
		if err := b.backup.Del(ctx, removed); err != nil {
			return err
		}
	}
	for _, it := range toDel {
		b.tree.Delete(it)
		if b.idx != nil {
			b.idx.remove(it.item.Hash(), it.seq)
		}
	}
	if wasFull && (b.maxSize == 0 || b.tree.Len() < b.maxSize) {
		resetSignal(&b.notFullCh)
	}
	return nil
}

func (b *btreeBacking[T]) NotEmpty(ctx context.Context) error {
	for {
		b.lk.rlock()
		if b.closed {
			b.lk.runlock()
			return ErrClosed
		}
		if b.tree.Len() > 0 {
			b.lk.runlock()
			return nil
		}
		wait := b.notEmptyCh
		b.lk.runlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

func (b *btreeBacking[T]) NotFull(ctx context.Context) error {
	for {
		b.lk.rlock()
		if b.closed {
			b.lk.runlock()
			return ErrClosed
		}
		if b.maxSize == 0 || b.tree.Len() < b.maxSize {
			b.lk.runlock()
			return nil
		}
		wait := b.notFullCh
		b.lk.runlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

func (b *btreeBacking[T]) Len() int64 {
	b.lk.rlock()
	defer b.lk.runlock()
	return int64(b.tree.Len())
}

func (b *btreeBacking[T]) Close(ctx context.Context) error {
	b.lk.lock()
	defer b.lk.unlock()
	if b.closed {
		return nil
	}
	var err error
	if b.backup != nil {
		err = b.backup.Close(ctx)
	}
	b.closed = true
	close(b.notEmptyCh)
	close(b.notFullCh)
	return err
}

func (b *btreeBacking[T]) Clear(ctx context.Context) error {
	b.lk.lock()
	defer b.lk.unlock()
	if b.closed {
		return ErrClosed
	}
	if b.backup != nil {
		if err := b.backup.Clear(ctx); err != nil {
			return err
		}
	}
	wasFull := b.maxSize > 0 && b.tree.Len() >= b.maxSize
	b.tree.Clear()
	if b.idx != nil {
		b.idx = newBtreeIndex[T]()
	}
	if wasFull {
		resetSignal(&b.notFullCh)
	}
	return nil
}

func (b *btreeBacking[T]) All(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		b.lk.rlock()
		defer b.lk.runlock()
		if b.closed {
			var zero T
			yield(zero, ErrClosed)
			return
		}
		var zero T
		b.tree.Scan(func(it seqItem[T]) bool {
			select {
			case <-ctx.Done():
				yield(zero, context.Cause(ctx))
				return false
			default:
			}
			return yield(it.item, nil)
		})
	}
}

func (b *btreeBacking[T]) AllCOW(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		b.lk.rlock()
		if b.closed {
			b.lk.runlock()
			yield(zero, ErrClosed)
			return
		}
		// Yield in order while no writer waits. Once a writer is waiting (or after the
		// first such item), collect the remainder under the read lock without yielding,
		// then release and finish from the copy so the writer can proceed.
		var snap []T
		contended := false
		stop := false
		b.tree.Scan(func(it seqItem[T]) bool {
			if contended || b.lk.writeWanted() {
				contended = true
				snap = append(snap, it.item)
				return true
			}
			select {
			case <-ctx.Done():
				yield(zero, context.Cause(ctx))
				stop = true
				return false
			default:
			}
			if !yield(it.item, nil) {
				stop = true
				return false
			}
			return true
		})
		b.lk.runlock()
		if stop {
			return
		}
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
	}
}
