package queue

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"time"

	"github.com/go-json-experiment/json"
	"github.com/gostdlib/base/concurrency/sync"
	bctx "github.com/gostdlib/base/context"
	bolt "go.etcd.io/bbolt"
)

// bboltFlushInterval is the upper bound on how long a buffered item waits before being
// committed; group commit normally flushes much sooner.
const bboltFlushInterval = 100 * time.Millisecond

var (
	bboltItemsBucket = []byte("items")
	// errStopIter terminates a bbolt ForEach/cursor walk early. It never escapes a method.
	errStopIter = errors.New("stop iteration")
)

// bboltHooks is a per-instance set of test seams. Tests set fields on a specific
// *bboltBacking[T] before any flusher work runs; both fields are nil in production.
// Per-instance (rather than package globals) so concurrent tests on different queues
// do not step on each other.
type bboltHooks struct {
	// faultAfterBackup, when non-nil, is invoked inside Pop and Del immediately after the
	// backup has been mirrored but before the on-disk delete is applied; a non-nil return
	// aborts the transaction, exercising the Restore compensation path.
	faultAfterBackup func() error
	// commitStart, when non-nil, is invoked at the very start of commit(), before any
	// backup mirror or db.Update, so a test can pin the flusher mid-commit and race a
	// concurrent Clear/Close against it. commit() runs only in the single flusher
	// goroutine, so a plain func is race-free here.
	commitStart func()
}

// flushResult is shared by every Push whose items joined one buffered batch. The flusher
// sets err then closes done; waiters read err after done is closed.
type flushResult struct {
	done chan struct{}
	err  error
}

// clearCmd is a synchronous Clear request routed to the flusher goroutine. Routing
// through the flusher serializes Clear with all in-flight commits: any item the
// flusher is about to commit (or is mid-commit on) is drained first, then the bucket
// is deleted. Without this routing a Clear concurrent with a mid-commit Push would
// delete the bucket before the commit's db.Update landed, letting the item reappear.
type clearCmd struct {
	ctx  context.Context
	done chan error
}

func jsonDecode[T any](data []byte) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		var zero T
		return zero, err
	}
	return v, nil
}

// diskCodecRequirer is implemented by item types whose default JSON encoding cannot
// round-trip (Value, via its function fields). NewBboltFIFO/NewBboltPriority reject such
// a type with ErrCodecRequired unless WithCodec supplies a codec.
type diskCodecRequirer interface {
	requiresDiskCodec()
}

// bboltBacking is an on-disk queue backed by go.etcd.io/bbolt, used for both FIFO and
// priority. Each item is stored in the "items" bucket under a key built by keyOf:
//   - FIFO:     8-byte big-endian per-bucket sequence number
//   - priority: 8-byte big-endian Item.Priority() followed by the 8-byte sequence number
//
// bbolt iterates keys in byte-lexicographic order, so the head of the queue is the
// bucket's first key: insert order for FIFO, lowest Item.Priority value (insert order
// breaking ties) for priority. The priority key is a fixed 16 bytes, so it is inherently
// prefix-free.
//
// Items are encoded with github.com/go-json-experiment/json. Recovery is automatic: on
// Open the existing database is the source of truth.
//
// Semantics (blocking, hydration, backup mirror) mirror fifo, with one on-disk Hydrate
// exception: items are only loaded from the backup when the database is empty (a non-empty
// database is the source of truth and would otherwise be duplicated). On a non-empty
// (restart) database no items are loaded from the backup, but Backup.OnLoad is still
// driven once per persisted item, in stored order, so callers rebuilding external state
// get the same callback they would for an in-memory backing. lk, maxSize and maxBatch are
// injected by New via setQueueLock, setMaxSize and setMaxBatch before any other use; the
// flush goroutine is started by setQueueLock so it never races the lock injection.
type bboltBacking[T Item[T]] struct {
	lk    *qlock
	db    *bolt.DB
	keyOf func(v T, seq uint64) []byte
	idx   *bboltIndex
	count int64
	// inflight is the number of items admitted into the staging buffer but not yet
	// committed (buffered or snapshot-in-flight). Guarded by lk like count. The bounded
	// maxSize admission gate tests count+inflight so concurrent Pushes whose items are
	// still buffered cannot collectively overshoot maxSize. (Named distinctly from
	// qlock.pending, which counts writers blocked on the lock.)
	inflight int64
	maxSize  int
	maxBatch int
	priority bool
	notFull  *signal
	notEmpty *signal
	closed   bool
	backup   Backup[T]

	// Write-staging buffer. push appends here and blocks until the flusher commits the
	// batch. buf, cur and flushReq are guarded by lk; cur.done/err follow the
	// flushResult protocol. The flusher runs in flushGroup and stops when flushCancel
	// is called (close()).
	buf         []T
	cur         *flushResult
	flushReq    chan struct{}
	clearReq    chan *clearCmd
	flushCtx    context.Context
	flushCancel context.CancelFunc
	flushGroup  sync.Group

	// On-disk codec from WithCodec; nil falls back to the default JSON encoding.
	encode func(dst *bytes.Buffer, v T) error
	decode func(src []byte, dst *T) error

	// hooks holds optional test seams. Both fields are nil in production.
	hooks bboltHooks
}

// encodeItem serializes v for storage using the WithCodec encoder if set, else the
// default JSON encoding.
func (p *bboltBacking[T]) encodeItem(v T) ([]byte, error) {
	if p.encode != nil {
		var buf bytes.Buffer
		if err := p.encode(&buf, v); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return json.Marshal(v)
}

// decodeItem is the inverse of encodeItem.
func (p *bboltBacking[T]) decodeItem(data []byte) (T, error) {
	if p.decode != nil {
		var v T
		if err := p.decode(data, &v); err != nil {
			var zero T
			return zero, err
		}
		return v, nil
	}
	return jsonDecode[T](data)
}

// bboltIndex maps Item.Hash() to the bbolt storage keys in that bucket, so Exists/Del
// only Get+decode the bucket members instead of scanning the whole table. nil when WithIndex
// is not set. Stored keys are the exact bbolt keys, so bucket.Delete is O(log n).
type bboltIndex struct {
	m map[uint64][][]byte
}

func newBboltIndex() *bboltIndex {
	return &bboltIndex{m: map[uint64][][]byte{}}
}

func (x *bboltIndex) add(hash uint64, storageKey []byte) {
	x.m[hash] = append(x.m[hash], storageKey)
}

func (x *bboltIndex) remove(hash uint64, storageKey []byte) {
	s := x.m[hash]
	for i := range s {
		if bytes.Equal(s[i], storageKey) {
			s[i] = s[len(s)-1]
			x.m[hash] = s[:len(s)-1]
			break
		}
	}
	if len(x.m[hash]) == 0 {
		delete(x.m, hash)
	}
}

func (x *bboltIndex) bucket(hash uint64) [][]byte {
	return x.m[hash]
}

// bboltFIFOKey orders by insert sequence only (FIFO).
func bboltFIFOKey[T Item[T]](_ T, seq uint64) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, seq)
	return out
}

// bboltPriorityKey orders by Item.Priority() with insert sequence as a tiebreak. The
// fixed 16-byte width (8-byte priority || 8-byte seq) is inherently prefix-free.
func bboltPriorityKey[T Item[T]](v T, seq uint64) []byte {
	out := make([]byte, 16)
	binary.BigEndian.PutUint64(out[:8], v.Priority())
	binary.BigEndian.PutUint64(out[8:], seq)
	return out
}

// NewBboltFIFO returns an on-disk FIFO Backing backed by go.etcd.io/bbolt, keyed by insert
// sequence. The database lives in "queue.db" under root and is the source of truth on reopen.
// It accepts WithIndex(). As a disk based queue, it is imporant to optimize with batch pushes and pulls.
// If doing Del(), Exists(), this can be extremely slow without WithIndex(). If using a Backup, it can be better
// to choose a new location for root on restart and using WithNoSync(), WithBoltFreelistMap() and WithNoFreelistSync()
func NewBboltFIFO[T Item[T]](ctx context.Context, root *os.Root, options ...BackingOption) (Backing[T], error) {
	o, err := applyBackingOptions(callBboltFIFO, options)
	if err != nil {
		return nil, err
	}
	return newBboltBacking(ctx, root, o, bboltFIFOKey[T], false)
}

// NewBboltPriority returns an on-disk priority Backing backed by go.etcd.io/bbolt, keyed by
// Item.Priority with insert sequence as the tiebreak. The database lives in "queue.db" under
// root and is the source of truth on reopen. It accepts WithIndex. // If doing Del(), Exists(), this can be
// extremely slow without WithIndex(). If using a Backup, it can be better to choose a new location for root on
// restart and using WithNoSync(), WithBoltFreelistMap() and WithNoFreelistSync().
func NewBboltPriority[T Item[T]](ctx context.Context, root *os.Root, options ...BackingOption) (Backing[T], error) {
	o, err := applyBackingOptions(callBboltPriority, options)
	if err != nil {
		return nil, err
	}
	return newBboltBacking(ctx, root, o, bboltPriorityKey[T], true)
}

func newBboltBacking[T Item[T]](ctx context.Context, root *os.Root, o backingOpts, keyOf func(v T, seq uint64) []byte, priority bool) (Backing[T], error) {
	var encode func(*bytes.Buffer, T) error
	var decode func([]byte, *T) error
	if o.codecEncode != nil {
		e, okE := o.codecEncode.(func(*bytes.Buffer, T) error)
		d, okD := o.codecDecode.(func([]byte, *T) error)
		if !okE || !okD {
			return nil, errors.New("queue: WithCodec encoder/decoder do not match the queue item type")
		}
		encode, decode = e, d
	}
	var zero T
	if _, needs := any(zero).(diskCodecRequirer); needs && encode == nil {
		return nil, ErrCodecRequired
	}

	bopts := &bolt.Options{
		Timeout:         o.boltTimeout,
		NoSync:          o.boltNoSync,
		NoFreelistSync:  o.boltNoFreelistSync,
		NoGrowSync:      o.boltNoGrowSync,
		PreLoadFreelist: o.boltPreLoadFreelist,
		Mlock:           o.boltMlock,
		MmapFlags:       o.boltMmapFlags,
		InitialMmapSize: o.boltInitialMmapSize,
		PageSize:        o.boltPageSize,
		OpenFile:        o.boltOpenFile,
	}
	if o.boltFreelistMap {
		bopts.FreelistType = bolt.FreelistMapType
	}
	db, err := bolt.Open(filepath.Join(root.Name(), "queue.db"), 0o600, bopts)
	if err != nil {
		return nil, err
	}
	var count int64
	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bboltItemsBucket)
		if err != nil {
			return err
		}
		count = int64(b.Stats().KeyN)
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	p := &bboltBacking[T]{
		lk:       &qlock{},
		db:       db,
		keyOf:    keyOf,
		count:    count,
		priority: priority,
		notFull:  newSignal(),
		notEmpty: newSignal(),
		cur:      &flushResult{done: make(chan struct{})},
		flushReq: make(chan struct{}, 1),
		clearReq: make(chan *clearCmd, 1),
		encode:   encode,
		decode:   decode,
	}
	if o.index {
		p.idx = newBboltIndex()
		if err := p.rebuildIndex(); err != nil {
			db.Close()
			return nil, err
		}
	}
	fctx, cancel := bctx.WithCancel(ctx)
	p.flushCtx = fctx
	p.flushCancel = cancel
	p.flushGroup = bctx.Pool(ctx).Group()
	// The flush goroutine is started by setQueueLock so it never observes the placeholder
	// lock created above being swapped for the shared one injected by New.
	return p, nil
}

func (p *bboltBacking[T]) private() {}

// setQueueLock injects the shared lock and starts the flush goroutine. New calls this once,
// before any other use, so the goroutine never races the lock assignment.
func (p *bboltBacking[T]) setQueueLock(lk *qlock) {
	p.lk = lk
	// Launch the flusher with a non-cancelable accounting context: the worker pool
	// skips a job whose launch context is already canceled, which would happen if the
	// queue is created then closed before the pooled worker starts (the flusher would
	// never run, nor do its final flush). WithoutCancel strips only the
	// cancellation/deadline while preserving context values (the gostdlib pool, logger
	// and OTEL tracer ride on ctx). flushLoop's own shutdown is driven by p.flushCtx,
	// which it selects on.
	p.flushGroup.Go(context.WithoutCancel(p.flushCtx), func(context.Context) error { return p.flushLoop(p.flushCtx) })
}

func (p *bboltBacking[T]) setMaxBatch(n int) error {
	if n < 1 {
		return errors.New("max batch must be at least 1")
	}
	p.maxBatch = n
	return nil
}

func (p *bboltBacking[T]) setMaxSize(n int) error {
	if n < 0 {
		return errors.New("max size must be at least 0")
	}
	p.maxSize = n
	return nil
}

// flushLoop is the group-commit loop: it commits the write buffer as soon as it is
// signaled (every append signals), so the first buffered item flushes immediately and
// items arriving during an in-flight commit coalesce into the next one. The ticker is an
// upper bound. On ctx cancel (close) it does a final flush and returns.
func (p *bboltBacking[T]) flushLoop(ctx context.Context) error {
	t := time.NewTicker(bboltFlushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			p.doFlush(ctx)
			return nil
		case <-p.flushReq:
			p.doFlush(ctx)
		case cmd := <-p.clearReq:
			cmd.done <- p.doClear(cmd.ctx)
		case <-t.C:
			p.doFlush(ctx)
		}
	}
}

// doFlush snapshots the buffer under the lock, rotates the flushResult so new pushes join
// the next batch, commits the snapshot, then signals the waiters of this batch.
func (p *bboltBacking[T]) doFlush(ctx context.Context) {
	p.lk.lock()
	if len(p.buf) == 0 {
		p.lk.unlock()
		return
	}
	snap := p.buf
	p.buf = nil
	r := p.cur
	p.cur = &flushResult{done: make(chan struct{})}
	p.lk.unlock()

	r.err = p.commit(ctx, snap)
	close(r.done)
}

// commit mirrors the batch to the backup (if any), writes it to bbolt in one transaction,
// then updates count/index/notEmpty under the lock. A backup or write failure fails the
// whole batch (returned to every waiter); the items are not made visible.
func (p *bboltBacking[T]) commit(ctx context.Context, snap []T) error {
	if p.hooks.commitStart != nil {
		p.hooks.commitStart()
	}
	if p.backup != nil {
		if err := p.backup.Push(ctx, snap); err != nil {
			p.lk.lock()
			p.inflight -= int64(len(snap))
			p.lk.unlock()
			if p.notFull.HasWaiters() {
				p.notFull.Signal()
			}
			return err
		}
	}
	type idxEnt struct {
		hash uint64
		sk   []byte
	}
	var ents []idxEnt
	err := p.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bboltItemsBucket)
		for _, v := range snap {
			data, err := p.encodeItem(v)
			if err != nil {
				return err
			}
			seq, err := b.NextSequence()
			if err != nil {
				return err
			}
			sk := p.keyOf(v, seq)
			if err := b.Put(sk, data); err != nil {
				return err
			}
			if p.idx != nil {
				ents = append(ents, idxEnt{hash: v.Hash(), sk: sk})
			}
		}
		return nil
	})
	if err != nil {
		p.lk.lock()
		p.inflight -= int64(len(snap))
		p.lk.unlock()
		if p.notFull.HasWaiters() {
			p.notFull.Signal()
		}
		return err
	}
	p.lk.lock()
	wasEmpty := p.count == 0
	p.count += int64(len(snap))
	p.inflight -= int64(len(snap))
	if p.idx != nil {
		for _, e := range ents {
			p.idx.add(e.hash, e.sk)
		}
	}
	p.lk.unlock()
	if wasEmpty && p.notEmpty.HasWaiters() {
		p.notEmpty.Signal()
	}
	return nil
}

// rebuildIndex scans the existing database once and populates the in-memory index. Called
// at construction so a reopened on-disk queue (the source of truth) has a consistent index.
func (p *bboltBacking[T]) rebuildIndex() error {
	return p.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bboltItemsBucket).ForEach(func(k, data []byte) error {
			item, err := p.decodeItem(data)
			if err != nil {
				return err
			}
			sk := append([]byte(nil), k...)
			p.idx.add(item.Hash(), sk)
			return nil
		})
	})
}

// Hydrate implements Backing.Hydrate().
func (p *bboltBacking[T]) Hydrate(ctx context.Context, b Backup[T]) error {
	p.lk.lock()
	defer p.lk.unlock()
	if p.count > 0 {
		// The on-disk store already holds items: this is a restart restoring from
		// bbolt's own durable storage rather than from the backup. Still drive OnLoad
		// for each persisted item (in stored order) so callers rebuilding external
		// structures get the same callback they would for an in-memory backing.
		if err := p.db.View(func(tx *bolt.Tx) error {
			c := tx.Bucket(bboltItemsBucket).Cursor()
			for k, data := c.First(); k != nil; k, data = c.Next() {
				v, err := p.decodeItem(data)
				if err != nil {
					return err
				}
				if err := b.OnLoad(ctx, v); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		p.backup = b
		return nil
	}
	type pending struct {
		v    T
		data []byte
	}
	var buf []pending
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		sks := make([][]byte, 0, len(buf))
		err := p.db.Update(func(tx *bolt.Tx) error {
			bkt := tx.Bucket(bboltItemsBucket)
			for i := range buf {
				seq, err := bkt.NextSequence()
				if err != nil {
					return err
				}
				sk := p.keyOf(buf[i].v, seq)
				if err := bkt.Put(sk, buf[i].data); err != nil {
					return err
				}
				sks = append(sks, sk)
			}
			return nil
		})
		if err != nil {
			return err
		}
		p.count += int64(len(buf))
		if p.idx != nil {
			for i := range buf {
				p.idx.add(buf[i].v.Hash(), sks[i])
			}
		}
		buf = buf[:0]
		return nil
	}
	for v, err := range b.RangeAll(ctx) {
		if err != nil {
			return err
		}
		if err := validateKindOne(p.priority, v); err != nil {
			return err
		}
		if err := b.OnLoad(ctx, v); err != nil {
			return err
		}
		data, err := p.encodeItem(v)
		if err != nil {
			return err
		}
		buf = append(buf, pending{v: v, data: data})
		if len(buf) == hydrateBatch {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	p.backup = b
	return nil
}

// Push implements Backing.Push(). It stages the batch in the write buffer (capacity
// maxBatch) and blocks until the flusher commits it. Over-maxBatch batches are rejected by
// Queue.Push before reaching here, so len(vs) <= maxBatch always holds; on a bounded queue
// a batch larger than maxSize returns ErrBatchTooLarge. When concurrent pushes have
// momentarily filled the shared maxBatch buffer this Push flushes and retries (it always
// fits once drained); that pre-buffer wait is context-cancelable, but once the items are
// buffered the wait for the flush to commit is not.
func (p *bboltBacking[T]) Push(ctx context.Context, vs []T) error {
	if err := validateKind(p.priority, vs); err != nil {
		return err
	}
	for {
		p.lk.lock()
		if p.closed {
			p.lk.unlock()
			return ErrClosed
		}
		if p.maxSize > 0 && int64(len(vs)) > int64(p.maxSize) {
			p.lk.unlock()
			return ErrBatchTooLarge
		}
		if p.maxSize > 0 && p.count+p.inflight+int64(len(vs)) > int64(p.maxSize) {
			if err := p.notFull.Wait(ctx, p.lk.unlock); err != nil {
				return p.closedOrCause(ctx)
			}
			continue
		}
		if len(p.buf)+len(vs) <= p.maxBatch {
			p.buf = append(p.buf, vs...)
			p.inflight += int64(len(vs))
			r := p.cur
			p.lk.unlock()
			// Group commit: signal on every append, not just when full. The flusher
			// commits immediately when idle; pushes that arrive while a commit is in
			// flight coalesce into the next batch. The interval timer is only an upper
			// bound, and the maxBatch cap only forces backpressure.
			select {
			case p.flushReq <- struct{}{}:
			default:
			}
			<-r.done // not ctx-cancelable once buffered
			return r.err
		}
		// Buffer lacks room: trigger a flush and wait for it to drain. This wait is
		// context-cancelable because the items are not yet buffered.
		r := p.cur
		p.lk.unlock()
		select {
		case p.flushReq <- struct{}{}:
		default:
		}
		select {
		case <-r.done:
		case <-ctx.Done():
			return p.closedOrCause(ctx)
		}
	}
}

// Pop implements Backing.Pop().
func (p *bboltBacking[T]) Pop(ctx context.Context, n int) ([]T, error) {
	for {
		p.lk.lock()
		if p.closed {
			p.lk.unlock()
			return nil, ErrClosed
		}
		if p.count > 0 {
			k := n
			if int64(k) > p.count {
				k = int(p.count)
			}
			out := make([]T, 0, k)
			var sks [][]byte
			if p.idx != nil {
				sks = make([][]byte, 0, k)
			}
			// Peek + mirror + delete must be one transaction: the flush goroutine's
			// commit() runs db.Update without holding p.lk, so a separate read txn
			// could see a lower-priority item inserted between the peek and the delete.
			backupDeleted := false
			err := p.db.Update(func(tx *bolt.Tx) error {
				c := tx.Bucket(bboltItemsBucket).Cursor()
				// Pass 1: read the first k items in order without removing them.
				key, data := c.First()
				for i := 0; i < k; i++ {
					if key == nil {
						return ErrEmpty
					}
					dv, err := p.decodeItem(data)
					if err != nil {
						return err
					}
					out = append(out, dv)
					if p.idx != nil {
						sks = append(sks, append([]byte(nil), key...))
					}
					key, data = c.Next()
				}
				// Mirror the exact popped items to the backup before removing them.
				// An error here rolls back the txn so nothing is deleted.
				if p.backup != nil {
					if err := p.backup.Del(ctx, out); err != nil {
						return err
					}
					backupDeleted = true
				}
				if p.hooks.faultAfterBackup != nil {
					if e := p.hooks.faultAfterBackup(); e != nil {
						return e
					}
				}
				// Pass 2: delete the first k items.
				for i := 0; i < k; i++ {
					dk, _ := c.First()
					if dk == nil {
						return ErrEmpty
					}
					if err := c.Delete(); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				// The txn rolled back (or the commit did not durably land) but the
				// backup was already mirrored, so the items are still on disk.
				// Restore them to the front of the backup (these were the head
				// items) to keep it a true mirror, then report the failure; if the
				// restore also fails, both errors are returned and the backup is
				// genuinely out of sync.
				if backupDeleted {
					if rerr := p.backup.Restore(ctx, out); rerr != nil {
						p.lk.unlock()
						return nil, errors.Join(err, rerr)
					}
				}
				p.lk.unlock()
				return nil, err
			}
			if p.idx != nil {
				for i, v := range out {
					p.idx.remove(v.Hash(), sks[i])
				}
			}
			p.count -= int64(k)
			// Freed capacity: gated on HasWaiters; see Pop.
			if p.notFull.HasWaiters() {
				p.notFull.Signal()
			}
			p.lk.unlock()
			return out, nil
		}
		if err := p.notEmpty.Wait(ctx, p.lk.unlock); err != nil {
			return nil, p.closedOrCause(ctx)
		}
	}
}

// Peek implements Backing.Peek().
func (p *bboltBacking[T]) Peek(ctx context.Context) (T, bool, error) {
	var zero T
	p.lk.rlock()
	defer p.lk.runlock()
	if p.closed {
		return zero, false, ErrClosed
	}
	if p.count == 0 {
		return zero, false, nil
	}
	var v T
	err := p.db.View(func(tx *bolt.Tx) error {
		_, data := tx.Bucket(bboltItemsBucket).Cursor().First()
		if data == nil {
			return ErrEmpty
		}
		dv, err := p.decodeItem(data)
		if err != nil {
			return err
		}
		v = dv
		return nil
	})
	if err != nil {
		return zero, false, err
	}
	return v, true, nil
}

// Exists implements Backing.Exists().
func (p *bboltBacking[T]) Exists(ctx context.Context, v T) (bool, error) {
	p.lk.rlock()
	defer p.lk.runlock()
	if p.closed {
		return false, ErrClosed
	}
	found := false
	err := p.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bboltItemsBucket)
		if p.idx != nil {
			for _, sk := range p.idx.bucket(v.Hash()) {
				data := b.Get(sk)
				if data == nil {
					continue
				}
				item, err := p.decodeItem(data)
				if err != nil {
					return err
				}
				if item.Equal(v) {
					found = true
					return errStopIter
				}
			}
			return nil
		}
		return b.ForEach(func(_, data []byte) error {
			item, err := p.decodeItem(data)
			if err != nil {
				return err
			}
			if item.Equal(v) {
				found = true
				return errStopIter
			}
			return nil
		})
	})
	if err != nil && !errors.Is(err, errStopIter) {
		return false, err
	}
	return found, nil
}

// Del implements Backing.Del().
func (p *bboltBacking[T]) Del(ctx context.Context, v []T) error {
	p.lk.lock()
	defer p.lk.unlock()
	if p.closed {
		return ErrClosed
	}
	if p.count == 0 {
		return nil
	}
	// Collect the keys and items matching v without deleting them. The lock is held
	// for the whole Del, so the database cannot change before the delete below.
	var keys [][]byte
	var items []T
	// khash[i] is the index-bucket hash for keys[i] (only filled on the indexed path,
	// which is the only path that later calls idx.remove).
	var khash []uint64
	if err := p.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bboltItemsBucket)
		if p.idx != nil {
			// Scan only the buckets for the distinct hashes in v; dedup collected
			// keys by string(sk) so a slot matched by duplicate/same-hash elements
			// of v is removed once.
			seenHash := make(map[uint64]struct{}, len(v))
			seenKey := make(map[string]struct{})
			for e := range v {
				h := v[e].Hash()
				if _, ok := seenHash[h]; ok {
					continue
				}
				seenHash[h] = struct{}{}
				for _, sk := range p.idx.bucket(h) {
					if _, ok := seenKey[string(sk)]; ok {
						continue
					}
					data := b.Get(sk)
					if data == nil {
						continue
					}
					item, err := p.decodeItem(data)
					if err != nil {
						return err
					}
					if matchesAny(item, v) {
						seenKey[string(sk)] = struct{}{}
						keys = append(keys, append([]byte(nil), sk...))
						items = append(items, item)
						khash = append(khash, h)
					}
				}
			}
			return nil
		}
		return b.ForEach(func(k, data []byte) error {
			item, err := p.decodeItem(data)
			if err != nil {
				return err
			}
			if matchesAny(item, v) {
				keys = append(keys, append([]byte(nil), k...))
				items = append(items, item)
			}
			return nil
		})
	}); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	// Mirror the exact removed items to the backup before deleting them.
	if p.backup != nil {
		if err := p.backup.Del(ctx, items); err != nil {
			return err
		}
	}
	err := p.db.Update(func(tx *bolt.Tx) error {
		if p.hooks.faultAfterBackup != nil {
			if e := p.hooks.faultAfterBackup(); e != nil {
				return e
			}
		}
		b := tx.Bucket(bboltItemsBucket)
		for _, k := range keys {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// The backup was already mirrored but the database delete failed (or did not
		// durably commit), so the items are still on disk. Restore them to the backup
		// to keep it a true mirror, then report the failure.
		if p.backup != nil {
			if rerr := p.backup.Restore(ctx, items); rerr != nil {
				return errors.Join(err, rerr)
			}
		}
		return err
	}
	removed := len(keys)
	deleted := keys
	if p.idx != nil {
		for i, sk := range deleted {
			p.idx.remove(khash[i], sk)
		}
	}
	p.count -= int64(removed)
	// Freed capacity: gated on HasWaiters; see Pop.
	if p.notFull.HasWaiters() {
		p.notFull.Signal()
	}
	return nil
}

// NotEmpty implements Backing.NotEmpty().
func (p *bboltBacking[T]) NotEmpty(ctx context.Context) error {
	for {
		p.lk.rlock()
		if p.closed {
			p.lk.runlock()
			return ErrClosed
		}
		if p.count > 0 {
			p.lk.runlock()
			return nil
		}
		if err := p.notEmpty.Wait(ctx, p.lk.runlock); err != nil {
			return p.closedOrCause(ctx)
		}
	}
}

// NotFull implements Backing.NotFull().
func (p *bboltBacking[T]) NotFull(ctx context.Context) error {
	for {
		p.lk.rlock()
		if p.closed {
			p.lk.runlock()
			return ErrClosed
		}
		if p.maxSize == 0 || p.count < int64(p.maxSize) {
			p.lk.runlock()
			return nil
		}
		if err := p.notFull.Wait(ctx, p.lk.runlock); err != nil {
			return p.closedOrCause(ctx)
		}
	}
}

// Len implements Backing.Len().
func (p *bboltBacking[T]) Len() int64 {
	p.lk.rlock()
	defer p.lk.runlock()
	return p.count
}

// closedOrCause returns ErrClosed if the backing has been closed, else the ctx cause.
// Used in the ctx.Done() arm of a blocked wait so Close deterministically wins a race
// with ctx cancellation.
func (p *bboltBacking[T]) closedOrCause(ctx context.Context) error {
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
func (p *bboltBacking[T]) Close(ctx context.Context) error {
	p.lk.lock()
	if p.closed {
		p.lk.unlock()
		return nil
	}
	p.closed = true
	p.lk.unlock()

	// Stop the flusher (it does a final flush of the remaining buffer on ctx cancel,
	// unblocking any pushers still waiting on their batch) and wait for it to finish
	// before touching the db.
	p.flushCancel()
	gErr := p.flushGroup.Wait(ctx)

	p.lk.lock()
	var bErr error
	if p.backup != nil {
		bErr = p.backup.Close(ctx)
	}
	cErr := p.db.Close()
	p.lk.unlock()
	// Wake any parked Wait callers; they re-acquire p.lk, see closed, return ErrClosed.
	p.notEmpty.Signal()
	p.notFull.Signal()
	return errors.Join(gErr, bErr, cErr)
}

// Clear implements Backing.Clear(). It is routed through the single-threaded flusher
// so it serializes with all in-flight commits: any item whose Push has buffered or is
// mid-commit is drained first (its Push returns success and its items are briefly in
// the queue) and only then is the bucket deleted. No in-flight commit can land items
// after the delete.
func (p *bboltBacking[T]) Clear(ctx context.Context) error {
	cmd := &clearCmd{ctx: ctx, done: make(chan error, 1)}
	select {
	case p.clearReq <- cmd:
	case <-p.flushCtx.Done():
		return ErrClosed
	case <-ctx.Done():
		return context.Cause(ctx)
	}
	select {
	case err := <-cmd.done:
		return err
	case <-p.flushCtx.Done():
		// The flusher exited (Close) before processing cmd.
		return ErrClosed
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// doClear runs only in the flusher goroutine. It first drains any buffered items
// (so their Pushes return success and the items are reflected in p.count), then
// deletes the bucket under p.lk. Because it runs single-threaded with all commit()s,
// no concurrent commit can land items after the delete.
//
// The drain step uses the flusher lifetime ctx (p.flushCtx), not the Clear caller's
// ctx: the items being committed belong to other callers' Pushes, and a Clear
// caller's ctx cancellation must not fail them via backup.Push. The Clear-specific
// operations below (backup.Clear) do use the caller's ctx — that one the caller owns.
func (p *bboltBacking[T]) doClear(ctx context.Context) error {
	p.doFlush(p.flushCtx)

	p.lk.lock()
	defer p.lk.unlock()
	if p.closed {
		return ErrClosed
	}
	if p.count == 0 {
		return nil
	}
	if p.backup != nil {
		if err := p.backup.Clear(ctx); err != nil {
			return err
		}
	}
	err := p.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(bboltItemsBucket); err != nil {
			return err
		}
		_, err := tx.CreateBucket(bboltItemsBucket)
		return err
	})
	if err != nil {
		return err
	}
	p.count = 0
	if p.idx != nil {
		p.idx = newBboltIndex()
	}
	// Freed capacity: gated on HasWaiters; see Pop.
	if p.notFull.HasWaiters() {
		p.notFull.Signal()
	}
	return nil
}

// All implements Backing.All().
func (p *bboltBacking[T]) All(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		p.lk.rlock()
		defer p.lk.runlock()
		var zero T
		if p.closed {
			yield(zero, ErrClosed)
			return
		}
		p.db.View(func(tx *bolt.Tx) error {
			c := tx.Bucket(bboltItemsBucket).Cursor()
			for k, data := c.First(); k != nil; k, data = c.Next() {
				select {
				case <-ctx.Done():
					yield(zero, context.Cause(ctx))
					return errStopIter
				default:
				}
				v, err := p.decodeItem(data)
				if err != nil {
					yield(zero, err)
					return errStopIter
				}
				if !yield(v, nil) {
					return errStopIter
				}
			}
			return nil
		})
	}
}

// AllCOW implements Backing.AllCOW().
func (p *bboltBacking[T]) AllCOW(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		p.lk.cowEnter()
		defer p.lk.cowExit()
		p.lk.rlock()
		if p.closed {
			p.lk.runlock()
			yield(zero, ErrClosed)
			return
		}
		// snap holds the raw encoded remainder, copied once a writer is waiting. We
		// store undecoded bytes so the read lock (which excludes that writer) is held
		// only for the scan, not the decode+consume phase that follows — honoring the
		// copy-on-write contract: a waiting writer proceeds during iteration.
		var snap [][]byte
		contended := false
		stop := false
		var rerr error
		p.db.View(func(tx *bolt.Tx) error {
			c := tx.Bucket(bboltItemsBucket).Cursor()
			for k, data := c.First(); k != nil; k, data = c.Next() {
				if contended || p.lk.writeWanted() {
					contended = true
					snap = append(snap, append([]byte(nil), data...))
					continue
				}
				v, err := p.decodeItem(data)
				if err != nil {
					rerr = err
					return errStopIter
				}
				select {
				case <-ctx.Done():
					yield(zero, context.Cause(ctx))
					stop = true
					return errStopIter
				default:
				}
				if !yield(v, nil) {
					stop = true
					return errStopIter
				}
			}
			return nil
		})
		p.lk.runlock()
		if rerr != nil {
			yield(zero, rerr)
			return
		}
		if stop {
			return
		}
		for _, data := range snap {
			v, err := p.decodeItem(data)
			if err != nil {
				yield(zero, err)
				return
			}
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
