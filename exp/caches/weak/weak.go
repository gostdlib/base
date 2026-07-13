// Package weak provides a thread-safe weak pointer cache that automatically cleans up entries
// when the weakly referenced objects are garbage collected. It supports basic operations like Get, Set and Del.
// The internal implementation uses sharded maps for concurrency and performance.
// The cache has no size limit and relies on Go's runtime to manage memory once objects are no longer referenced.
// This can be used to implement caches on top of more durable storage layers via the filler, setter, and deleter functions.
// You can then wrap calls for database Set(), Get(), and Delete() operations with this cache to improve performance
// while keeping memory usage in check via weak references. The deleter runs on explicit Del calls and forced maxTTL
// evictions, but not when the GC reclaims a value (the value is gone from memory; the durable copy is untouched).
package weak

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
	"weak"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/exp/caches/weak/internal/hashmap"
	cacheMetrics "github.com/gostdlib/base/exp/caches/weak/internal/metrics"
	"github.com/gostdlib/base/exp/caches/weak/internal/shardmap"
	"github.com/gostdlib/base/telemetry/otel/metrics"
	"github.com/tidwall/btree"
)

type ttlEntry[K comparable, V any] struct {
	// hold is the time until which we should keep a strong reference to the value.
	hold        time.Time
	expireAfter time.Time
	key         K
	// seq is a monotonic sequence number that breaks ties between entries that share an expireAfter deadline.
	// K is comparable but not ordered, so seq gives the expireAfter tree a total order and prevents distinct keys
	// with identical deadlines from colliding (the tree replaces on comparator-equality).
	seq uint64

	value *V
}

// expireKey identifies a node in the expireAfter tree by the two fields ttlEntry.less compares: the forced-eviction
// deadline and the tie-breaking sequence number. It carries no strong value, so the expireIndex that stores it never
// pins a value the way a tree node does.
type expireKey struct {
	expireAfter time.Time
	seq         uint64
}

func (e ttlEntry[K, V]) less(than ttlEntry[K, V]) bool {
	switch {
	case e.expireAfter.Before(than.expireAfter):
		return true
	case than.expireAfter.Before(e.expireAfter):
		return false
	default:
		// Identical deadlines: fall back to seq so distinct keys keep a stable total order and neither replaces
		// the other in the expireAfter tree.
		return e.seq < than.seq
	}
}

// flightResult carries the result of a Get through the singleflight so followers receive the leader's value.
// The error travels through singleflight's own error return, so it is not carried as a field here.
type flightResult[V any] struct {
	value *V
	ok    bool
}

// Cache is a weak pointer cache.
type Cache[K comparable, V any] struct {
	m          *shardmap.Map[K, V]
	ttl        time.Duration
	maxTTL     time.Duration
	interval   time.Duration
	useFlights bool
	getFlight  sync.Flight[K, flightResult[V]]

	// ttlLock guards ttlMap and expireAfter. Lock-ordering invariant: ttlLock is always acquired before any shard
	// mutex (held internally by m). Any path that holds both must take ttlLock first and hold it across the pair
	// (see set, getOrFill, Del, ttlExpire); no path may acquire a shard mutex and then ttlLock, so the two never
	// form a cycle.
	ttlLock     sync.Mutex
	ttlMap      hashmap.Map[K, ttlEntry[K, V]]
	expireAfter *btree.BTreeG[ttlEntry[K, V]]
	// expireIndex maps a key to the exact expireAfter-tree node key (the deadline and seq that ttlEntry.less compares)
	// for the entry that migrated out of the ttlMap into the tree. Del consults it to prune that node directly instead
	// of leaving its strong value pinned until the far-off maxTTL tick. It stores only the tree-key fields, never the
	// strong value, so it never pins a value. Written when ttlExpire migrates an entry into the tree, read and cleared
	// by Del, and cleared by the tick when it removes the node it points at. Guarded by ttlLock like ttlMap/expireAfter.
	expireIndex map[K]expireKey
	// seq assigns each ttlEntry a unique, monotonically increasing sequence number so entries with identical
	// expireAfter deadlines get a stable total order in the expireAfter tree.
	seq atomic.Uint64

	filler  Filler[K, V]
	setter  Setter[K, V]
	deleter Deleter[K]

	// cleanupCtx is the constructor-scope context captured at New. The runtime.AddCleanup closures run at GC time,
	// long after any request-scoped context has ended, so they use this context for metric emission instead of
	// pinning the context that happened to be live at Set/Get time.
	cleanupCtx context.Context

	// onReclaim is the runtime.AddCleanup callback registered for every stored value. It is built once in New so
	// each Set/fill reuses the same closure instead of allocating a new one per store.
	onReclaim func(K)

	metrics *cacheMetrics.Cache
}

// newExpireAfterTree builds the expireAfter btree used to schedule maxTTL forced evictions. It is the single source
// of truth for the tree's comparator and options so New and the white-box tests cannot drift apart.
func newExpireAfterTree[K comparable, V any]() *btree.BTreeG[ttlEntry[K, V]] {
	return btree.NewBTreeGOptions(func(a, b ttlEntry[K, V]) bool { return a.less(b) }, btree.Options{Degree: 2, NoLocks: true})
}

type opts struct {
	ttl, maxTTL time.Duration
	interval    time.Duration
	useFlights  bool
	less        any
	// filler, setter and deleter are of type any to avoid always using generics on opts.
	// They will be type asserted when used.
	filler  any
	setter  any
	deleter any
}

// Option is an option for New().
type Option func(o opts) (opts, error)

// WithTTL sets the time-to-live for entries in the cache and the cleanup interval.
// Entries older than ttl will be removed during cleanup.
// The ttl must be at least 1 second and is the minimum duration an entry will be kept in the cache.
// The maxTTL parameter is optional and can be set to 0 to disable it. If set, ttl cannot be greater than maxTTL.
// This causes entries to be removed after maxTTL even if they are still being referenced. The maxTTL cleanup happens
// during the same cleanup process as the regular ttl, so it may stick around for up to interval longer than maxTTL.
// The interval parameter sets how often the cleanup runs, which must be at least 1 second.
func WithTTL(ttl, maxTTL, interval time.Duration) Option {
	return func(o opts) (opts, error) {
		if interval < 1*time.Second {
			return o, fmt.Errorf("cleanup interval must be at least 1 second")
		}
		if ttl <= 0 {
			return o, fmt.Errorf("ttl must be greater than 0")
		}
		if ttl < 1*time.Second {
			return o, fmt.Errorf("ttl must be at least 1 second")
		}
		if maxTTL > 0 && ttl > maxTTL {
			return o, fmt.Errorf("ttl cannot be greater than maxTTL")
		}
		o.ttl = ttl
		o.maxTTL = maxTTL
		o.interval = interval
		return o, nil
	}
}

// WithSingleFlight enables the use of the singleflight package for Get operations.
// This adds another lock on Get operations, but prevents multiple concurrent
// Get() calls for the same key from causing multiple loads of the same value. Use this to
// prevent thundering herd problems when loading values from the cache. If not using WithFiller() to
// retrieve missing values, this option will likely slow operations down instead of speeding them up.
func WithSingleFlight() Option {
	return func(o opts) (opts, error) {
		o.useFlights = true
		return o, nil
	}
}

// Filler is a function that will be run if the cache needs to fill a missing value. If this function
// returns a value, it will be set in the cache and returned to the caller. If it returns an error, the Get call will fail with
// that error.
type Filler[K comparable, V any] = shardmap.Filler[K, V]

// WithFiller sets a custom filler function for the cache. This is used to load missing values into the cache on Get calls.
func WithFiller[K comparable, V any](f Filler[K, V]) Option {
	return func(o opts) (opts, error) {
		o.filler = f
		return o, nil
	}
}

// Setter is a function that will be run when setting a value in the cache. If this function
// returns an error, the value will not be set. This is used to set values in durable storage when they are added to the cache.
type Setter[K comparable, V any] = shardmap.Setter[K, V]

// WithSetter sets a custom setter function for the cache. This is used to set values in durable storage when they are added to the cache.
func WithSetter[K comparable, V any](s Setter[K, V]) Option {
	return func(o opts) (opts, error) {
		o.setter = s
		return o, nil
	}
}

// Deleter is a function that will be run when deleting a value from the cache. If this function
// returns an error, the value will not be deleted. This is used to delete values in durable storage when they are
// removed from the cache.
type Deleter[K comparable] = shardmap.Deleter[K]

// WithDeleter sets a custom deleter function for the cache. The deleter runs on explicit Del calls and on forced
// maxTTL evictions. It does not run when the GC reclaims a value: the value is already gone from memory and the
// durable copy is left untouched. On Del the deleter runs even when the key holds no live entry, since a weak entry
// can be reclaimed at any time while its durable copy remains. If the deleter returns an error, the in-memory delete
// is aborted (mirroring how a Setter error aborts a store): Del returns the error, and a forced maxTTL eviction logs
// it and keeps the entry, retrying the eviction on every cleanup interval until the deleter succeeds (or the GC
// reclaims the value or a Set replaces it). On the first failure it logs at Error and drops the strong value it was
// holding for that entry while keeping the eviction scheduled; later failing retries for the same entry log at Debug.
// Dropping the value makes the GC-reclaim exit real: a permanently failing deleter no longer pins the value forever,
// so once nothing else references it the GC reclaims it and the schedule then clears on the next tick. The deleter
// runs under the shard write lock, and on caches with a TTL it also runs under the TTL lock, the same tradeoff the
// Setter makes.
func WithDeleter[K comparable](d Deleter[K]) Option {
	return func(o opts) (opts, error) {
		o.deleter = d
		return o, nil
	}
}

// WithDeDupe enables de-duplication of values in the cache based on the provided less function.
// When enabled, the cache will ensure that only one instance of a value exists in the cache
// for any given key, based on the comparison provided by the less function.
// This can help reduce memory usage when many identical values are stored in the cache.
// The less function must tolerate a weak.Pointer whose Value() returns nil: values can be garbage collected before
// tree maintenance runs, so both the DeleteIfNil cleanup and the maxTTL eviction path can pass an already-collected
// pointer. Implementations must handle a nil Value() without panicking (for example by ordering nil consistently).
func WithDeDupe[V any](less func(a, b weak.Pointer[V]) bool) Option {
	return func(o opts) (opts, error) {
		o.less = less
		return o, nil
	}
}

func fillMetricWrap[K comparable, V any](metrics *cacheMetrics.Cache, f Filler[K, V]) Filler[K, V] {
	return func(ctx context.Context, k K) (*V, bool, error) {
		v, ok, err := f(ctx, k)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, nil
		}
		metrics.Fills.Add(ctx, 1)
		return v, true, nil
	}
}

// New creates a new Cache with the given options. Name is a unique identifier for the cache, used for metrics.
func New[K comparable, V any](ctx context.Context, name string, options ...Option) (*Cache[K, V], error) {
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	o := opts{}
	for _, option := range options {
		var err error
		o, err = option(o)
		if err != nil {
			return nil, fmt.Errorf("weak cache %q: applying option: %w", name, err)
		}
	}

	mp := context.MeterProvider(ctx)
	meter := mp.Meter(metrics.MeterName(2) + "/" + name)
	cm := cacheMetrics.New(meter)

	var m *shardmap.Map[K, V]
	if o.less != nil {
		m = shardmap.New[K, V](o.less.(func(a, b weak.Pointer[V]) bool))
	} else {
		m = shardmap.New[K, V](nil)
	}

	c := &Cache[K, V]{
		m:          m,
		useFlights: o.useFlights,
		cleanupCtx: ctx,
		metrics:    cm,
	}
	// Build the GC cleanup callback once so every Set/fill reuses it. Only a cleanup that actually removes the bucket
	// decrements cache_items; if Del or a maxTTL eviction already removed the key (and decremented), this is a no-op so
	// the gauge is not double-decremented.
	c.onReclaim = func(k K) {
		if _, deleted := c.m.DeleteIfNil(k); deleted {
			c.metrics.CacheItems.Add(c.cleanupCtx, -1)
		}
	}
	if o.filler != nil {
		c.filler = fillMetricWrap(c.metrics, o.filler.(Filler[K, V]))
	}
	if o.setter != nil {
		c.setter = o.setter.(Setter[K, V])
	}
	if o.deleter != nil {
		c.deleter = o.deleter.(Deleter[K])
	}
	if o.maxTTL > 0 {
		c.maxTTL = o.maxTTL
		c.expireAfter = newExpireAfterTree[K, V]()
		c.expireIndex = make(map[K]expireKey)
	}
	if o.ttl > 0 {
		c.ttl = o.ttl
		c.interval = o.interval
		// ttlExpire is a long-lived ticker loop outside any request/response cycle, so it runs on the background
		// task manager. Once (not Run) is the right call: the loop only ends when ctx is canceled and must not be
		// restarted after that.
		err := context.Tasks(ctx).Once(ctx, "weak cache "+name+" ttlExpire", func(ctx context.Context) error {
			c.ttlExpire(ctx)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("weak cache %q: starting ttlExpire task: %w", name, err)
		}
	}

	return c, nil
}

// ttlExpire runs in a background goroutine to clean up expired entries in the ttlMap.
// This map is holding values with a regular pointer to prevent the weak reference from
// being collected before the ttl expires. Once the TTL expires, the entry is deleted from the ttlMap,
// which allows the weak reference in the main map to be collected by the GC if not used.
func (m *Cache[K, V]) ttlExpire(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	deletions := []K{}
	// failed collects entries whose forced eviction failed this tick. Their strong value is dropped and they are
	// re-Set into the tree after the walk (mutating the tree during DeleteAscend is unsafe), keeping the schedule.
	failed := []ttlEntry[K, V]{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			var evicted int64
			m.ttlLock.Lock()
			for k, v := range m.ttlMap.All() {
				if v.hold.Before(now) {
					deletions = append(deletions, k)
					if !v.expireAfter.IsZero() {
						// Drop any older node this key already left in the tree before overwriting its index entry, so the
						// index-write below cannot orphan it (its strong value would then linger until its own deadline and
						// Del could no longer find it). recordStore prunes on re-Set, so the index is normally empty here;
						// this is the belt-and-suspenders guard for the same install invariant at the migration site. Any
						// stale entry carries an older seq than v (each install gets a fresh seq), so pruneExpired only ever
						// removes a genuine orphan, never the node just Set below.
						m.pruneExpired(k)
						m.expireAfter.Set(v)
						// Index the migrated node so Del can prune it directly instead of waiting for the maxTTL tick.
						m.expireIndex[k] = expireKey{expireAfter: v.expireAfter, seq: v.seq}
					}
				}
			}
			for _, k := range deletions {
				m.ttlMap.Delete(k)
			}

			if m.expireAfter != nil {
				// Start from the zero time so every entry up to now is visited. The tree is ordered by expireAfter,
				// so iteration stops once we reach an entry whose expireAfter is not yet before now.
				m.expireAfter.DeleteAscend(
					ttlEntry[K, V]{},
					func(item ttlEntry[K, V]) btree.Action {
						if !item.expireAfter.Before(now) {
							return btree.Stop
						}
						removed, err := m.delWithTTL(ctx, item.key, item.expireAfter)
						if err != nil {
							// The deleter failed. Keep the schedule so the next tick retries, but drop the strong value
							// (via the re-Set below) so a permanently failing deleter no longer pins the value forever;
							// the GC can then reclaim it, making the WithDeleter doc's GC-reclaim exit reachable. Log
							// loudly on the first failure (value still held) and quietly afterwards to bound the
							// per-tick spam a poison key would otherwise produce.
							if item.value != nil {
								context.Log(ctx).Error("weak cache: deleter failed during maxTTL forced eviction, retrying next tick", "error", err)
							} else {
								context.Log(ctx).Debug("weak cache: deleter still failing during maxTTL forced eviction, retrying next tick", "error", err)
							}
							failed = append(failed, item)
							return btree.Keep
						}
						if removed {
							evicted++
						}
						// The node is leaving the tree; clear its index entry, but only if the index still points at
						// this exact node (a later re-Set + migration may have repointed the key to a newer node).
						if ek, ok := m.expireIndex[item.key]; ok && ek.seq == item.seq {
							delete(m.expireIndex, item.key)
						}
						return btree.Delete
					},
				)
				// Re-Set each failed entry with its strong value dropped. The re-Set replaces the same node (its
				// less() key, expireAfter+seq, is unchanged), so the schedule and the index entry both survive while
				// the value is released. delWithTTL reads only key+expireAfter, so the nil value does not affect retry.
				for i := range failed {
					failed[i].value = nil
					m.expireAfter.Set(failed[i])
				}
			}
			m.ttlLock.Unlock()
			// Emit one batched cache_items decrement after releasing ttlLock rather than one per eviction under it.
			if evicted > 0 {
				m.metrics.CacheItems.Add(ctx, -evicted)
			}
			deletions = deletions[:0]
			failed = failed[:0]
		}
	}
}

// Set assigns a value to a key.
// Returns the previous value, or false when no value was assigned. If you
// Set a nil value, it is equivalent to Delete.
func (m *Cache[K, V]) Set(ctx context.Context, k K, v *V) (prev *V, replaced bool, err error) {
	if v == nil {
		return m.Del(ctx, k)
	}

	return m.set(ctx, k, v)
}

// ttlDeadlines computes the min-TTL hold deadline and the maxTTL forced-eviction deadline from a single now so the
// deadline written to the ttlMap matches the one handed to the shard store (that match is what lets forced eviction
// find the entry). Both are the zero time when TTL is disabled, keeping the TTL-free path syscall-free.
func (m *Cache[K, V]) ttlDeadlines() (hold, maxTTLDeadline time.Time) {
	if m.ttl > 0 {
		now := time.Now()
		hold = now.Add(m.ttl)
		if m.expireAfter != nil {
			maxTTLDeadline = now.Add(m.maxTTL)
		}
	}
	return hold, maxTTLDeadline
}

// recordStore performs the shared post-store TTL bookkeeping for set() and getOrFill(). stored is the value actually
// held under the key: the caller's value, or, under WithDeDupe, the dedup representative it merged onto. The min-TTL
// hold and the GC cleanup are both attached to stored so a deduped key pins and reclaims the object its bucket
// references rather than the discarded duplicate. The caller must hold ttlLock (when m.ttl > 0) across the shard store
// and this call so a concurrent Del cannot interleave between the shard entry and the ttlMap hold; recordStore writes
// the ttlMap hold and releases ttlLock, then registers the GC cleanup and emits the dedup and cache_items metrics
// outside every lock. fresh and deduped come from the shard store: only a physically fresh bucket bumps cache_items
// (overwriting an existing bucket, even one whose weak pointer had gone nil, keeps the same key), staying symmetric
// with the Del and maxTTL eviction decrements.
func (m *Cache[K, V]) recordStore(ctx context.Context, k K, stored *V, hold, maxTTLDeadline time.Time, fresh, deduped, storedUnchanged bool) {
	if m.ttl > 0 {
		// If a prior entry for k already migrated into the expireAfter tree (its hold expired and it moved out of the
		// ttlMap), that stale node still pins its old strong value until the far-off maxTTL tick. This store overwrites
		// the key, so prune the old node before installing the new hold; otherwise the replaced value stays pinned for
		// the remainder of the old maxTTL. Mirrors the prune Del does, and covers the nil-valued retry nodes a failing
		// deleter leaves in the tree so a Set clears their schedule immediately rather than on the next tick.
		if m.expireAfter != nil {
			m.pruneExpired(k)
		}
		m.ttlMap.Set(k, ttlEntry[K, V]{hold: hold, expireAfter: maxTTLDeadline, key: k, seq: m.seq.Add(1), value: stored}, time.Time{})
		m.ttlLock.Unlock()
	}
	// Register the GC cleanup only when this store changed the object the bucket references. A re-Set of the same key
	// that merges back onto the same live representative (WithDeDupe) leaves the stored object unchanged and already
	// has a (stored, k) cleanup registered, so registering another would just accumulate duplicate cleanups on a
	// long-lived representative. The ttlMap hold above is refreshed unconditionally, so the min-TTL still restarts on
	// every store regardless of this gate.
	if !storedUnchanged {
		runtime.AddCleanup[V, K](stored, m.onReclaim, k)
	}
	if deduped {
		m.metrics.Dedups.Add(ctx, 1)
	}
	if fresh {
		m.metrics.CacheItems.Add(ctx, 1)
	}
}

func (m *Cache[K, V]) set(ctx context.Context, k K, v *V) (prev *V, replaced bool, err error) {
	hold, maxTTLDeadline := m.ttlDeadlines()

	// Hold ttlLock across the shard store and the ttlMap hold insert so a concurrent Del cannot slip between the two:
	// without this, Del could delete the ttlMap hold set() just wrote and, finding no shard entry yet, do nothing,
	// after which set() would store the shard entry with no hold, leaving it without the min-TTL strong reference or
	// maxTTL eviction scheduling. Writing the shard entry first and the ttlMap hold only on a successful store also
	// means a setter error leaves no stale ttlMap entry behind. This keeps the ttlLock->shard-lock ordering used by
	// getOrFill, Del and ttlExpire; the setter now runs under ttlLock (via the shard write lock), an accepted tradeoff.
	if m.ttl > 0 {
		m.ttlLock.Lock()
	}
	res, err := m.m.Set(ctx, k, v, m.setter, maxTTLDeadline)
	if err != nil {
		if m.ttl > 0 {
			m.ttlLock.Unlock()
		}
		return nil, false, err
	}
	// Register cleanup and the min-TTL hold against the value actually stored under the key. With WithDeDupe that is
	// the dedup representative, not the caller's discarded duplicate, so the cleanup fires on the object the bucket
	// references and the hold pins it. Without WithDeDupe res.Stored is the caller's v, so nothing changes.
	m.recordStore(ctx, k, res.Stored, hold, maxTTLDeadline, res.Fresh, res.Deduped, res.StoredUnchanged)
	return res.Prev, res.Replaced, nil
}

// Get returns a value for a key.
// Returns false when no value has been assign for key.
func (m *Cache[K, V]) Get(ctx context.Context, k K) (value *V, ok bool, err error) {
	if m.useFlights {
		res, doErr, _ := m.getFlight.Do(ctx, k, func() (flightResult[V], error) {
			v, found, e := m.getOrFill(ctx, k)
			return flightResult[V]{value: v, ok: found}, e
		})
		value, ok, err = res.value, res.ok, doErr
	} else {
		value, ok, err = m.getOrFill(ctx, k)
	}
	if ok {
		m.metrics.CacheHits.Add(ctx, 1)
	} else {
		m.metrics.CacheMisses.Add(ctx, 1)
	}
	return value, ok, err
}

// getOrFill returns the value for k from the shard map. On a miss it runs the filler (if configured), stores the
// loaded value without clobbering a concurrent explicit Set, and, when it is the one that stored the value, runs
// the same TTL and cleanup bookkeeping that set() does so filler-loaded entries are force-evicted after maxTTL and
// reclaimed by the GC just like explicitly Set entries.
func (m *Cache[K, V]) getOrFill(ctx context.Context, k K) (value *V, ok bool, err error) {
	value, ok = m.m.Get(k)
	if ok {
		return value, true, nil
	}
	if m.filler == nil {
		return nil, false, nil
	}

	v, found, err := m.filler(ctx, k)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	if v == nil {
		// The filler reported a hit with a nil value. Caching a nil is pointless (a nil weak pointer always reads back
		// as a miss) and would panic: recordStore's runtime.AddCleanup rejects a nil pointer, and under WithSingleFlight
		// that panic in the leader takes down every follower too. Skip the store and return the filler's result
		// unchanged, matching the pre-refactor behavior of quietly returning (nil, true, nil) to the caller.
		return nil, true, nil
	}

	hold, maxTTLDeadline := m.ttlDeadlines()

	// Hold ttlLock across the shard store and the ttlMap hold insert so a concurrent Del cannot slip between the two:
	// without this, Del could delete the entry after SetIfNil made it visible but before the ttlMap hold was written,
	// leaving a stale strong-ref hold for a key no longer in the cache. This keeps the ttlLock->shard-lock ordering
	// used by set(), Del and ttlExpire.
	if m.ttl > 0 {
		m.ttlLock.Lock()
	}
	res, err := m.m.SetIfNil(ctx, k, v, maxTTLDeadline)
	switch {
	case err != nil:
		if m.ttl > 0 {
			m.ttlLock.Unlock()
		}
		return nil, false, err
	case !res.Ok:
		if m.ttl > 0 {
			m.ttlLock.Unlock()
		}
		// A concurrent writer won the store. SetIfNil read their live value under the shard lock, so return it
		// directly. Re-Getting here instead would risk a spurious miss if the GC collected the value in the gap.
		return res.Prev, true, nil
	}
	// We stored the value, so account for it exactly like set() does, holding and cleaning up the value actually
	// stored under the key (the dedup representative under WithDeDupe, otherwise the caller's v).
	m.recordStore(ctx, k, res.Stored, hold, maxTTLDeadline, res.Fresh, res.Deduped, res.StoredUnchanged)
	return res.Stored, true, nil
}

// Del deletes a value for a key. If a Deleter is configured it runs before the in-memory delete; a Deleter error is
// returned and the in-memory delete is aborted, leaving the entry in place.
// Returns the deleted value, or false when no value was assigned.
func (m *Cache[K, V]) Del(ctx context.Context, k K) (prev *V, deleted bool, err error) {
	// Hold ttlLock across both the shard delete and the ttlMap delete so a concurrent filler store (getOrFill) cannot
	// interleave its SetIfNil + ttlMap.Set between them and leave a stale strong-ref hold. This keeps the
	// ttlLock->shard-lock ordering used by set(), getOrFill and ttlExpire. The shard delete runs first so a deleter
	// error aborts Del before the ttlMap hold is touched, leaving the entry fully intact. The cache_items decrement is
	// deliberately emitted after ttlLock is released: it plays no part in the ttlMap+shard atomicity the monitor tests
	// enforce, so it stays out of the lock.
	if m.ttl > 0 {
		m.ttlLock.Lock()
	}

	prev, deleted, err = m.m.Delete(ctx, k, m.deleter)
	if err != nil {
		if m.ttl > 0 {
			m.ttlLock.Unlock()
		}
		return nil, false, err
	}
	if m.ttl > 0 {
		m.ttlMap.Delete(k)
		// If the entry already migrated into the expireAfter tree, its hold is gone from the ttlMap but the tree node
		// still holds the strong value until the maxTTL tick. Prune that exact node now so Del releases the value
		// immediately instead of leaving it pinned. The index points at the currently active node for k.
		if m.expireAfter != nil {
			m.pruneExpired(k)
		}
		m.ttlLock.Unlock()
	}
	// Delete reports removed==true whenever it physically took out a counted bucket, including one whose weak pointer
	// had already gone nil (prev == nil). Decrement on that so the gauge is not stranded high when the GC-cleanup path
	// later no-ops on the already-missing key.
	if deleted {
		m.metrics.CacheItems.Add(ctx, -1)
	}
	return prev, deleted, nil
}

// pruneExpired removes k's migrated node from the expireAfter tree, if one exists, and clears its index entry. An entry
// lands in the tree once its min-TTL hold expires (see ttlExpire), where its node keeps a strong reference to the value
// until the far-off maxTTL tick. Both Del and a re-store (recordStore) call this so an overwritten or deleted key stops
// pinning that value immediately instead of waiting for the tick. The expireIndex points at the currently active node
// for k, so constructing the tree key from the stored expireKey deletes exactly that node. The caller must hold ttlLock
// and must have confirmed m.expireAfter is non-nil (ttl-only caches have no tree or index).
func (m *Cache[K, V]) pruneExpired(k K) {
	if ek, ok := m.expireIndex[k]; ok {
		m.expireAfter.Delete(ttlEntry[K, V]{expireAfter: ek.expireAfter, seq: ek.seq})
		delete(m.expireIndex, k)
	}
}

// delWithTTL runs a forced maxTTL eviction for k. A non-nil err means the deleter returned an error and the entry must
// be retried; the caller keeps the entry in its expireAfter tree (dropping its strong value) so the next tick retries
// rather than stranding it. removed reports whether a counted bucket was physically taken out, letting the caller
// batch the cache_items decrement. delWithTTL reads only key and the deadline, never the entry's stored value, so the
// retry path works even after the value has been dropped.
func (m *Cache[K, V]) delWithTTL(ctx context.Context, k K, ttl time.Time) (removed bool, err error) {
	_, deleted, err := m.m.DeleteIfMaxTTL(ctx, k, ttl, m.deleter)
	if err != nil {
		return false, err
	}
	return deleted, nil
}

// Len returns the number of values in map. This is an approximation since keys may hold nil values that
// have not yet been cleaned up.
func (m *Cache[K, V]) Len() int {
	return m.m.Len()
}
