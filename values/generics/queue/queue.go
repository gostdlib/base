// Package queue provides a generic, thread-safe queue implementation with multiple backing options.
// The queue supports blocking and non-blocking operations. Both on-disk and in-memory queues support a backup interface
// that can be used for recovery in the event of a crash or other failure.
//
// The backing data structure is chosen by the caller and passed to New. Construct one with NewFIFO,
// NewBTreeFIFO, NewBtypeFIFO, NewPriority, NewBTreePriority, NewBboltFIFO or NewBboltPriority.
// WithIndex enables an in-memory hash index for O(1) Exists / O(log n) Del; it is honored by the
// priority B-Tree and the BoltDB backings, and by NewBTreeFIFO (which switches to a keyed B-Tree
// when indexed — the positional FIFO has no stable locator to index). WithBTreeWidth tunes the
// keyed B-Tree backings. Priority backings sort items by their Less method instead of insertion
// order.
package queue

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync/atomic"

	"github.com/gostdlib/base/concurrency/sync"
)

// qlock is the shared lock for a Queue and its backing. The mutex is only ever held for
// short critical sections — never across a channel/ctx wait. pending counts writers
// currently blocked on the write lock; a copy-on-write RangeAll watches it to decide
// when to snapshot the remainder and release so a waiting writer can proceed.
type qlock struct {
	mu      sync.RWMutex
	pending atomic.Int32
}

// lock acquires the write lock, recording the wait so writeWanted observes blocked writers.
func (l *qlock) lock() {
	l.pending.Add(1)
	l.mu.Lock()
	l.pending.Add(-1)
}

// unlock releases the write lock.
func (l *qlock) unlock() { l.mu.Unlock() }

// rlock acquires the read lock.
func (l *qlock) rlock() { l.mu.RLock() }

// runlock releases the read lock.
func (l *qlock) runlock() { l.mu.RUnlock() }

// writeWanted reports whether a writer is currently blocked waiting for the write lock.
// While a copy-on-write RangeAll holds the read lock a blocked writer cannot acquire, so
// pending stays > 0 until the reader releases — the signal cannot be missed.
func (l *qlock) writeWanted() bool { return l.pending.Load() > 0 }

const (
	// Unlimited is a constant that can be used to indicate that the queue should be unbounded.
	// This is the default if maxSize is < 1.
	Unlimited = 0

	// hydrateBatch is the number of items the on-disk backing accumulates before flushing
	// them to storage as one batch (one bbolt txn) during hydrate.
	hydrateBatch = 100
)

var (
	// ErrEmpty is returned when the queue is empty and there are no items to pop.
	ErrEmpty = errors.New("queue is empty")
	// ErrClosed is returned when an operation is attempted on a closed queue.
	ErrClosed = errors.New("queue is closed")
	// ErrBatchTooLarge is returned by Push when the batch exceeds the configured max
	// batch size (WithMaxBatch) or a bounded queue's maximum size.
	ErrBatchTooLarge = errors.New("batch larger than allowed maximum")
	// ErrPriorityRequired is returned when an item with Priority() == 0 is pushed onto a priority queue.
	ErrPriorityRequired = errors.New("priority queue requires items with Priority() > 0")
	// ErrPriorityNotAllowed is returned when an item with Priority() > 0 is pushed onto a FIFO queue.
	ErrPriorityNotAllowed = errors.New("FIFO queue requires items with Priority() == 0")
)

// Item is type constraint for items that can be stored in the queue.
// There are built in backing implementations that support Item for numeric types, string/[]byte types
// and for generic value types.
type Item[T any] interface {
	// Less returns true if the item is less than the other item.
	Less(T) bool
	// Equal returns true if the item is equal to the other item.
	Equal(T) bool
	// Priority returns the priority sort key. It must be order-consistent with Less:
	// if a.Less(b) then a.Priority() < b.Priority(); items with equal Priority are
	// ordered by insert sequence. Items pushed onto a priority queue must return a
	// value > 0; items pushed onto a FIFO queue must return 0 (the queue rejects a
	// Push that violates this). Only consulted by the on-disk priority backing
	// (NewBboltPriority); other backings sort via Less directly.
	Priority() uint64
	// Hash returns a value-derived bucket key for the WithIndex option. It must be
	// consistent with Equal: if a.Equal(b) then a.Hash() == b.Hash(). Collisions are
	// allowed; Equal still confirms a match.
	Hash() uint64
}

// Backup provides an implementation of a backup for the running queue. As items are written and removed from the
// queue, the backup will be updated to reflect the current state of the queue. This allows for recovery in the
// event of a crash or other failure. This can be useful even if using on disk queues, as this allows recovery
// from off disk backup. Backup calls are made before the main operation is completed. If the backup call fails,
// the main operation will not be attempted and the error will be returned to the caller. If the backup call succeeds
// but the main operation fails, we will attempt a rollback. It is important that the backup implementation have
// typed errors so that you can tell what to do in the event of a failure. Less important for in-memory queues,
// but for on-disk queues you can end in an inconsistent state if the backup succeeds but the main operation fails
// and we cannot roll back. In those cases, it is a good idea to either somehow deal with that the backup a
// queue items that on-disk does not or panic the server and restore from backup to get back to a consistent state.
type Backup[T Item[T]] interface {
	// Push pushes a batch of items onto the backup, mirroring a queue Push. Non-blocking:
	// the backup has no maximum size and cannot be full. Returns an error only on a write
	// failure.
	Push(ctx context.Context, vs []T) error
	// Del removes the given items from the backup, exactly one matching (Item.Equal)
	// occurrence per element of vs. It is called with the precise items removed from the
	// queue — those popped by a PopN and those deleted by a Del — so the backup stays a
	// true mirror regardless of the backing's ordering. An element with no match is a
	// no-op for that element.
	Del(ctx context.Context, vs []T) error
	// Restore re-inserts vs at the front of the backup, in vs order, undoing a Del whose
	// corresponding queue mutation then failed (the on-disk delete or its commit did not
	// land). It is the compensating counterpart of Del so the backup remains a true
	// mirror. For items removed from the head (a PopN) this restores order exactly; for
	// interior items removed by a Del the relative order of vs is preserved at the head
	// (exact positional restore is not possible).
	Restore(ctx context.Context, vs []T) error
	// Len returns the number of items in the queue.
	Len() int64
	// Close closes the queue and releases any resources associated with it. After calling Close, the queue should not be used.
	Close(ctx context.Context) error
	// Clear removes all items from the queue.
	Clear(ctx context.Context) error
	// RangeAll returns an iter.Seq2 that will range over the items in the queue. This should
	// be in the same order as the queue's RangeAll.
	RangeAll(ctx context.Context) iter.Seq2[T, error]
	// OnLoad is called once for each item the queue is hydrated with, in backing order:
	// the items restored from the backup or, for an on-disk backing restarting against an
	// already-populated store, the items recovered from durable storage. This allows
	// restoration to also do side effects such as adding entries to maps or other data
	// structures. A returned error aborts hydration (and thus New). It runs during New,
	// before the queue exists, so it must not perform operations on this queue or its
	// backing store; restrict it to external state.
	OnLoad(ctx context.Context, v T) error
}

// Queue is a generic queue that can be used to store any type of item. The specific implementation of the
// queue depends on the Backing passed to New(). Queue is thread-safe.
type Queue[T Item[T]] struct {
	lk       *qlock
	backing  Backing[T]
	backup   Backup[T]
	maxBatch int
}

type queueOptions struct {
	maxBatch int
	backup   any
}

func (o queueOptions) defaults() queueOptions {
	if o.maxBatch == 0 {
		o.maxBatch = 1000
	}
	return o
}

func validateOptions[T Item[T]](o queueOptions) (Backup[T], error) {
	if o.maxBatch < 1 {
		return nil, errors.New("max batch must be at least 1")
	}

	if o.backup == nil {
		return nil, nil
	}
	backup, ok := o.backup.(Backup[T])
	if !ok {
		return nil, errors.New("backup must implement Backup[T]")
	}

	return backup, nil
}

// validateKindOne checks that an item's Priority() matches the backing kind: a priority
// backing requires Priority() > 0, a FIFO backing requires Priority() == 0.
func validateKindOne[T Item[T]](priority bool, v T) error {
	switch {
	case priority && v.Priority() == 0:
		return ErrPriorityRequired
	case !priority && v.Priority() != 0:
		return ErrPriorityNotAllowed
	}
	return nil
}

// validateKind applies validateKindOne to every item in vs.
func validateKind[T Item[T]](priority bool, vs []T) error {
	for _, v := range vs {
		if err := validateKindOne(priority, v); err != nil {
			return err
		}
	}
	return nil
}

// Option is optional arguments for New().
type Option func(o queueOptions) queueOptions

// WithMaxBatch sets the maximum number of items a single Push may contain (default 1000).
// A Push with more items than this returns ErrBatchTooLarge. For the on-disk backing this
// is also the size of the write-staging buffer, so larger values trade memory for fewer,
// larger group commits. n must be >= 1.
func WithMaxBatch(n int) Option {
	return func(o queueOptions) queueOptions {
		o.maxBatch = n
		return o
	}
}

// WithBackup configures the queue to use the provided backup. b must be a Backup for the queue type. This allows
// for recovery in the event of a crash or other failure. This can be useful even if using on disk queues,
// as this allows recovery from off disk backup. If WithBackup is used, the queue will be updated to
// a starting state that matches the backup, and all operations on the queue will be reflected in the backup.
func WithBackup(b any) Option {
	return func(o queueOptions) queueOptions {
		o.backup = b
		return o
	}
}

// New creates a new Queue backed by a Backing.
// maxSize is the maximum number of items the queue can hold; a value < 1 (or Unlimited) makes the queue unbounded.
func New[T Item[T]](ctx context.Context, b Backing[T], maxSize int, options ...Option) (*Queue[T], error) {
	if b == nil {
		return nil, fmt.Errorf("backing cannot be nil")
	}

	opts := queueOptions{}
	for _, option := range options {
		opts = option(opts)
	}
	opts = opts.defaults()

	backup, err := validateOptions[T](opts)
	if err != nil {
		return nil, err
	}

	if maxSize < 1 {
		maxSize = 0
	}

	lk := &qlock{}
	b.setQueueLock(lk)
	if err := b.setMaxBatch(opts.maxBatch); err != nil {
		return nil, err
	}
	if err := b.setMaxSize(maxSize); err != nil {
		return nil, err
	}

	q := &Queue[T]{backing: b, backup: backup, lk: lk, maxBatch: opts.maxBatch}
	if backup != nil {
		if err := b.Hydrate(ctx, backup); err != nil {
			b.Close(ctx)
			return nil, err
		}
	}
	return q, nil
}

type opOptions struct {
	sideEffect func() error
}

// OpOption is an optional arugment for queue operations.
type OpOption func(opts opOptions) opOptions

// WithSideEffect adds a side effect to be called after the main operation is successful. If this fails,
// the main operation will not be rolled back, but the error will be returned to the caller.
func WithSideEffect(f func() error) OpOption {
	return func(opts opOptions) opOptions {
		opts.sideEffect = f
		return opts
	}
}

// Close closes the queue and releases any resources associated with it. After calling Close, the queue should not be used.
func (q *Queue[T]) Close(ctx context.Context, options ...OpOption) error {
	opts := opOptions{}
	for _, option := range options {
		opts = option(opts)
	}

	if err := q.backing.Close(ctx); err != nil {
		return err
	}

	if opts.sideEffect != nil {
		return opts.sideEffect()
	}
	return nil

}

// NotEmpty waits until the queue is not empty or the context is cancelled.
func (q *Queue[T]) NotEmpty(ctx context.Context, options ...OpOption) error {
	opts := opOptions{}
	for _, option := range options {
		opts = option(opts)
	}

	if err := q.backing.NotEmpty(ctx); err != nil {
		return err
	}

	if opts.sideEffect != nil {
		return opts.sideEffect()
	}
	return nil
}

// NotFull waits until the queue is not full or the context is cancelled.
func (q *Queue[T]) NotFull(ctx context.Context, options ...OpOption) error {
	opts := opOptions{}
	for _, option := range options {
		opts = option(opts)
	}

	if err := q.backing.NotFull(ctx); err != nil {
		return err
	}

	if opts.sideEffect != nil {
		return opts.sideEffect()
	}
	return nil
}

// Push pushes a batch of items onto the queue as a unit: either all items are pushed or
// none are. An empty or nil batch is a no-op that returns (true, nil). A batch with more
// than the configured max batch size (WithMaxBatch, default 1000) returns ErrBatchTooLarge,
// as does a batch larger than a bounded queue's maximum size. Otherwise Push blocks until
// the whole batch fits or the context is canceled, in which case context.Cause(ctx) is
// returned. Because the items can be pushed but the side effect can fail, the second return
// value indicates whether the batch was pushed: if false the error indicates why; if true
// the error reports the side effect's result.
func (q *Queue[T]) Push(ctx context.Context, vs []T, options ...OpOption) (ok bool, err error) {
	opts := opOptions{}
	for _, option := range options {
		opts = option(opts)
	}

	if len(vs) == 0 {
		if opts.sideEffect != nil {
			return true, opts.sideEffect()
		}
		return true, nil
	}

	if len(vs) > q.maxBatch {
		return false, ErrBatchTooLarge
	}

	if err := q.backing.Push(ctx, vs); err != nil {
		return false, err
	}

	if opts.sideEffect != nil {
		return true, opts.sideEffect()
	}
	return true, nil
}

// PopN removes and returns up to n items from the front of the queue. n must be >= 1 or
// this will panic. PopN blocks until at least one item is available (or the context
// is canceled, returning context.Cause(ctx)), then returns between 1 and n items —
// whatever is available without further blocking. The returned slice is non-empty on a
// nil error. If a side effect is configured it runs after the items are removed; its
// error is returned alongside the items (the items are still removed).
func (q *Queue[T]) PopN(ctx context.Context, n int, options ...OpOption) ([]T, error) {
	if n < 1 {
		panic("invalid argument: n must be >= 1")
	}
	opts := opOptions{}
	for _, option := range options {
		opts = option(opts)
	}

	items, err := q.backing.PopN(ctx, n)
	if err != nil {
		return nil, err
	}
	if opts.sideEffect != nil {
		return items, opts.sideEffect()
	}
	return items, nil
}

// Peek returns the item at the front of the queue without removing it. If the queue is empty the
// second return value will be false. If the queue is not empty, the second return value will be true
// and the first return value will be the item at the front of the queue.
func (q *Queue[T]) Peek(ctx context.Context, options ...OpOption) (v T, ok bool, err error) {
	opts := opOptions{}
	for _, option := range options {
		opts = option(opts)
	}

	v, ok, err = q.backing.Peek(ctx)
	if err != nil {
		return v, false, err
	}
	if opts.sideEffect != nil {
		return v, ok, opts.sideEffect()
	}
	return v, ok, nil
}

// Exists returns true if the item exists in the queue. This is useful for checking if an item is in the queue before
// pushing it onto the queue. If we have an index configured, this will use that. If its a btree, this will be
// O log(n). If standard array, this will be O(n), so if your list is large this can be problematic.
func (q *Queue[T]) Exists(ctx context.Context, v T, options ...OpOption) (bool, error) {
	opts := opOptions{}
	for _, option := range options {
		opts = option(opts)
	}

	exists, err := q.backing.Exists(ctx, v)
	if err != nil {
		return false, err
	}
	if opts.sideEffect != nil {
		return exists, opts.sideEffect()
	}
	return exists, nil
}

// Del removes every item from the queue that returns Item.Equal(e) == true for any element e of v
// (all matches, not just one). Duplicate elements in v are idempotent and an empty v is a no-op.
// If no items match, this returns a nil error.
func (q *Queue[T]) Del(ctx context.Context, v []T, options ...OpOption) error {
	opts := opOptions{}
	for _, option := range options {
		opts = option(opts)
	}

	if err := q.backing.Del(ctx, v); err != nil {
		return err
	}
	if opts.sideEffect != nil {
		return opts.sideEffect()
	}
	return nil
}

// matchesAny reports whether stored Equals any element of vs.
func matchesAny[T Item[T]](stored T, vs []T) bool {
	for i := range vs {
		if stored.Equal(vs[i]) {
			return true
		}
	}
	return false
}

// Len returns the number of items in the queue.
func (q *Queue[T]) Len() int64 {
	return q.backing.Len()
}

// Clear removes all items from the queue.
func (q *Queue[T]) Clear(ctx context.Context, options ...OpOption) error {
	opts := opOptions{}
	for _, option := range options {
		opts = option(opts)
	}
	if err := q.backing.Clear(ctx); err != nil {
		return err
	}
	if opts.sideEffect != nil {
		return opts.sideEffect()
	}
	return nil
}

// RangeAll returns an iter.Seq2 that ranges over the items in the queue. It holds the
// read lock for the entire iteration, so writers (Push/Pop/Del/Clear) block until the
// sequence is fully consumed or abandoned (early loop exit or context cancellation).
// Do not call a mutating Queue method from inside the loop on the same goroutine — that
// self-deadlocks; use RangeAllCOW if you need writers to make progress during iteration.
func (q *Queue[T]) RangeAll(ctx context.Context) iter.Seq2[T, error] {
	return q.backing.All(ctx)
}

// RangeAllCOW is like RangeAll but does not block writers for the whole iteration. It
// holds the read lock only until a writer is waiting; at that point it copies the
// remaining items into a slice, releases the lock, and finishes iterating over the
// copy while the writer proceeds. The snapshot is taken at the point of contention, so
// items yielded after that reflect the queue state at that moment, not later mutations.
// For on-disk backings the remainder is also copied into memory, which can be large.
func (q *Queue[T]) RangeAllCOW(ctx context.Context) iter.Seq2[T, error] {
	return q.backing.AllCOW(ctx)
}

// Backing is the underlying data structure that implements the queue. It is sealed to this
// package; construct one with a backing constructor and pass it to New:
type Backing[T Item[T]] interface {
	// setQueueLock sets the shared lock for the queue and its backing. This is only called once, by New(),
	// and should not be called by implementations directly. The queue and backing use the same lock so that, for example, a RangeAll
	// can hold the read lock while iterating over the backing's items, blocking writers until the iteration is done.
	setQueueLock(lk *qlock)
	// setMaxBatch sets the maximum number of items a single Push may contain (default 1000).
	// A Push with more items than this returns ErrBatchTooLarge. For the on-disk backing this is also
	// the size of the write-staging buffer, so larger values trade memory for fewer, larger group commits.
	// n must be >= 1. This should only be called by the New() constructor.
	setMaxBatch(n int) error
	// setMaxSize sets the maximum number of items the queue can hold. If the queue is bounded and a batch
	// is pushed that exceeds the maximum size, Push returns ErrBatchTooLarge. For unbounded queues, this is
	// a no-op. This should only be called by the New() constructor.
	setMaxSize(n int) error
	// Push pushes a batch of items as a unit (all or none). On a bounded queue an error is
	// returned if the batch cannot ever fit; otherwise it blocks until the whole batch fits
	// or the context is canceled (context.Cause(ctx)). The caller guarantees len(vs) > 0.
	Push(ctx context.Context, vs []T) error
	// PopN removes and returns up to n items from the front of the queue. It blocks until
	// at least one item is available or the context is canceled (context.Cause(ctx)),
	// then returns 1..n items. The caller guarantees n >= 1.
	PopN(ctx context.Context, n int) ([]T, error)
	// Peek returns the item at the front of the queue without removing it. If the queue is empty the
	// second return value will be false. If the queue is not empty, the second return value will be true
	// and the first return value will be the item at the front of the queue.
	Peek(ctx context.Context) (T, bool, error)
	// Exists returns true if the item exists in the queue. This is useful for checking if an item is in the queue before
	// pushing it onto the queue. We use bloom filters if available and priority also helps. If neither, we have to
	// do a linear scan of the queue, which is O(n) and not ideal. Errors are only for disk issues.
	Exists(ctx context.Context, v T) (bool, error)
	// Del removes every item from the queue that returns Item.Equal(e) == true for any element e of v
	// (all matches, not just one). Duplicate elements in v are idempotent and an empty v is a no-op.
	// If no items match, this returns a nil error. Errors are only for disk issues.
	Del(ctx context.Context, v []T) error
	// NotEmpty waits until the queue is not empty or the context is cancelled.
	NotEmpty(ctx context.Context) error
	// NotFull waits until the queue is not full or the context is cancelled.
	NotFull(ctx context.Context) error
	// Len returns the number of items in the queue.
	Len() int64
	// Close closes the queue and releases any resources associated with it.
	// After calling Close, the queue should not be used.
	Close(ctx context.Context) error
	// Clear removes all items from the queue. Errors are only for disk issues.
	Clear(ctx context.Context) error
	// All ranges over the items holding the read lock for the whole iteration.
	// Errors are only for disk issues or if the context is canceled.
	All(ctx context.Context) iter.Seq2[T, error]
	// AllCOW ranges over the items but, when a writer is waiting, copies the
	// remainder, releases the lock, and finishes from the copy.
	AllCOW(ctx context.Context) iter.Seq2[T, error]
	// Hydrate loads items from the backup into the backing, calling Backup.OnLoad for each loaded item,
	// then attaches the backup so future mutations mirror to it. Must only be called once,
	// before any other mutation. Items loaded during hydrate do not mirror back to the backup.
	Hydrate(ctx context.Context, b Backup[T]) error

	private() // seal the interface to this package
}
