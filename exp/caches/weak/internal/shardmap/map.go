// Package shardmap is a re-done version of Josh Baker's shardmap package. It switches out the hash
// from xxhash to maphash, uses generics and has a few other minor changes. It is a thread-safe.
// Based on Josh Baker's shardmap.
package shardmap

import (
	"context"
	"hash/maphash"
	"iter"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"weak"

	"github.com/gostdlib/base/exp/caches/weak/internal/btree"
	rhh "github.com/gostdlib/base/exp/caches/weak/internal/hashmap"
)

// Map is a hashmap. Like map[string]interface{}, but sharded and thread-safe.
type Map[K comparable, V any] struct {
	init   sync.Once
	cap    int
	shards int
	mus    []sync.RWMutex
	maps   []*rhh.Map[K, weak.Pointer[V]]

	tree *btree.BTreeG[weak.Pointer[V]]
	less func(a, b weak.Pointer[V]) bool
	// treeMu guards the dedup tree and the refs refcount map as a single unit. It is the innermost lock in the
	// ttlLock (weak.Cache) -> shard -> tree hierarchy: it is only ever acquired while a shard mutex is held (or,
	// for Clear, on its own after the shards have been reset) and never the reverse, so it cannot form a cycle.
	treeMu sync.Mutex
	// refs counts how many shard-map buckets currently reference each dedup representative. It is keyed by the
	// representative weak.Pointer's identity (which is stable even after the referent is collected), so a nil-valued
	// representative is still looked up exactly rather than by the comparator's least-ordering of nil. A store adds a
	// reference, a delete/replace drops one, and the tree entry is removed only when the last reference is dropped.
	refs map[weak.Pointer[V]]int

	count atomic.Int64

	seed maphash.Seed
}

// New returns a new hashmap with the specified capacity. This function is only
// needed when you must define a minimum capacity, otherwise just use:
//
//	var m shardmap.Map
func New[K comparable, V any](less func(a, b weak.Pointer[V]) bool) *Map[K, V] {
	var tree *btree.BTreeG[weak.Pointer[V]]
	var refs map[weak.Pointer[V]]int
	if less != nil {
		tree = btree.NewBTreeG[weak.Pointer[V]](less)
		refs = make(map[weak.Pointer[V]]int)
	}

	m := &Map[K, V]{tree: tree, less: less, refs: refs, cap: 1024}
	m.initDo()
	return m
}

// treeAdd inserts wp into the dedup tree (or finds the existing representative that compares equal to it), records one
// more reference to that representative, and returns it. dedup reports whether an existing equal representative was
// found rather than wp being inserted as a new one. Callers must hold treeMu and store the returned representative in
// the shard bucket so the refs count stays symmetric with the buckets.
func (m *Map[K, V]) treeAdd(wp weak.Pointer[V]) (rep weak.Pointer[V], dedup bool) {
	rep, dedup = m.tree.Set(wp)
	if dedup && rep.Value() == nil {
		// The equal representative already in the tree has had its referent collected (Value() == nil). Merging wp onto
		// it would store a dead pointer in the bucket, so a Get of the key would miss even though a live value was just
		// stored. Evict the corpse by comparator identity and drop its refs entry: any bucket still referencing it will
		// treeRelease harmlessly, because deleting the refs entry makes that release a no-op (n == 0 -> the tree.Delete
		// there hits the already-removed node and does nothing). Then re-Set wp so the live pointer becomes the new
		// representative, inserted fresh (dedup false) now that the dead node is gone.
		m.tree.Delete(rep)
		delete(m.refs, rep)
		rep, dedup = m.tree.Set(wp)
	}
	m.refs[rep]++
	return rep, dedup
}

// treeRelease drops one reference to the representative rep and removes it from the dedup tree once the last reference
// is gone. Callers must hold treeMu and pass the exact representative that was stored in the bucket being removed or
// replaced. Lookup is by rep's pointer identity, so a rep whose referent has been collected (Value() == nil) is still
// released correctly even though the comparator orders every nil-valued pointer as the least element.
func (m *Map[K, V]) treeRelease(rep weak.Pointer[V]) {
	n := m.refs[rep]
	switch {
	case n == 0:
		// rep is not tracked in refs, so treeAdd already evicted it as a dead representative (its tree node was
		// removed there) or it was fully released earlier. Do not delete by comparator here: the node is already gone,
		// and a comparator that equates a collected (nil-valued) pointer with live ones would otherwise remove a live
		// node keyed the same. Releasing a bucket that still references the evicted rep is a clean no-op.
		return
	case n == 1:
		delete(m.refs, rep)
		m.tree.Delete(rep)
	default:
		m.refs[rep] = n - 1
	}
}

// releaseRep drops one reference to rep's dedup representative, taking treeMu around the release, when this map has a
// dedup tree (a no-op otherwise). Callers hold the shard lock and must not already hold treeMu: it is for the delete
// paths, not Set/SetIfNil, which hold treeMu across their own treeAdd/treeRelease pair.
func (m *Map[K, V]) releaseRep(rep weak.Pointer[V]) {
	if m.tree == nil {
		return
	}
	m.treeMu.Lock()
	m.treeRelease(rep)
	m.treeMu.Unlock()
}

// Clear out all values from map
func (m *Map[K, V]) Clear() {
	m.initDo()
	for i := 0; i < m.shards; i++ {
		m.mus[i].Lock()
		c := m.count.Load()
		m.count.Store(c - int64(m.maps[i].Len()))
		m.maps[i] = rhh.New[K, weak.Pointer[V]](m.cap / m.shards)
		m.mus[i].Unlock()
	}
	if m.tree != nil {
		m.treeMu.Lock()
		m.tree.Clear()
		clear(m.refs)
		m.treeMu.Unlock()
	}
}

// Setter is a function that will be run when setting a value in the cache. If this function
// returns an error, the value will not be set.
type Setter[K comparable, V any] func(ctx context.Context, k K, v *V) error

// SetResult reports the outcome of a Set or a SetIfNil store; both return it so the two store paths share one shape
// and the caller reads named fields instead of a run of same-typed positional booleans.
//
// Prev is the previous live value under the key, or nil when the key was absent or its value had already been
// collected. For Set it is the value that was overwritten. For SetIfNil, when the store is refused because a live
// value is already present, it is that live value (the one the racing caller lost to), read under the shard lock so
// the loser can return it directly instead of re-reading and risking a spurious miss.
//
// Stored is the live value the key references after a successful store: the caller's value, or, under WithDeDupe, the
// existing representative the caller's value merged onto. It is meaningful only when the store happened (Set always
// stores; SetIfNil stores only when Ok). Callers attach the GC cleanup and the min-TTL strong hold to Stored, not to
// their own value, so a deduped key's bookkeeping tracks the object its bucket actually references rather than the
// discarded duplicate. If the representative's value was already collected at store time, Stored falls back to the
// caller's value so a store never merges its bookkeeping onto a dead representative.
//
// Ok reports whether the store happened. Set always sets it true; SetIfNil sets it false when a live value was
// already present (the store was refused and Prev holds that value). Replaced reports whether a previous live value
// existed (Prev != nil). Fresh reports whether the store created a physically new bucket rather than overwriting an
// existing bucket, even one whose weak pointer had gone nil; it mirrors the internal count so callers can keep their
// own item counters symmetric with Len(). Deduped reports that the dedup tree already held an equal representative,
// letting the caller emit the dedup metric outside the shard lock.
//
// StoredUnchanged reports that the store left the bucket referencing the exact same object it already held (the old
// and new stored weak pointers have the same identity), which happens on a re-Set of the same key that merges back
// onto the same live representative. The caller uses it to skip re-registering the GC cleanup for a (stored, key) pair
// it already registered, so a long-lived representative does not accumulate duplicate cleanups. It is never set on a
// fresh bucket (there was no prior object to match).
type SetResult[V any] struct {
	Prev            *V
	Stored          *V
	Ok              bool
	Replaced        bool
	Fresh           bool
	Deduped         bool
	StoredUnchanged bool
}

// Set assigns a value to a key. The maxTTL is the deadline after which the entry may be forcibly evicted; the
// caller computes it so that the same deadline is used everywhere. Pass the zero time.Time to disable it.
func (m *Map[K, V]) Set(ctx context.Context, key K, value *V, setter Setter[K, V], maxTTL time.Time) (SetResult[V], error) {
	m.initDo()

	shard := m.choose(key)
	m.mus[shard].Lock()
	if setter != nil {
		if err := setter(ctx, key, value); err != nil {
			m.mus[shard].Unlock()
			return SetResult[V]{}, err
		}
	}
	wp := weak.Make(value)
	stored := value
	var dedup bool
	if m.tree != nil {
		m.treeMu.Lock()
		wp, dedup = m.treeAdd(wp)
		if dedup {
			stored = storedRep(wp, value)
		}
	}
	oldWP, hadBucket := m.maps[shard].Set(key, wp, maxTTL)
	if m.tree != nil {
		if hadBucket {
			// The bucket previously referenced oldWP's representative; drop that reference now that wp replaced it.
			// When old and new share a representative this is a decrement/increment wash that keeps the entry.
			m.treeRelease(oldWP)
		}
		m.treeMu.Unlock()
	}
	m.mus[shard].Unlock()
	prev := oldWP.Value()
	fresh := !hadBucket
	if fresh {
		m.count.Add(1)
	}
	// The bucket's stored object is unchanged when it overwrote a bucket that already referenced this exact weak
	// pointer (same identity), i.e. a re-Set of the key that merged back onto the same representative.
	storedUnchanged := hadBucket && oldWP == wp
	return SetResult[V]{Prev: prev, Stored: stored, Ok: true, Replaced: prev != nil, Fresh: fresh, Deduped: dedup, StoredUnchanged: storedUnchanged}, nil
}

// storedRep returns the value the caller should treat as the stored object after a dedup merge onto rep. rep is the
// existing representative that the caller's value was merged onto; it is read into a strong local here (under treeMu,
// with the shard lock held) so the caller can hold the representative itself alive rather than the discarded
// duplicate. If rep's referent was already collected, it falls back to fallback (the caller's value) so a store never
// merges its cleanup and TTL hold onto a dead representative.
func storedRep[V any](rep weak.Pointer[V], fallback *V) *V {
	if live := rep.Value(); live != nil {
		return live
	}
	return fallback
}

// SetIfNil assigns a value to a key only if the current value's weak pointer is nil or the key does not exist. It
// returns a SetResult (the same shape Set returns) so the two store paths stay unified. The maxTTL is the deadline
// after which the entry may be forcibly evicted; the caller computes it so the same deadline is used everywhere. Pass
// the zero time.Time to disable it. SetResult.Ok reports whether the value was stored; when the store is refused
// because a live value is already present, SetResult.Prev returns that live value (read under the shard lock) so a
// caller that lost the race can return it directly instead of re-reading, avoiding a spurious miss if the GC collects
// the value in the gap. On a successful store SetResult.Stored is the live value the key now references (the caller's
// value, or the dedup representative it merged onto). Fresh and Deduped carry the same meaning as in Set.
func (m *Map[K, V]) SetIfNil(ctx context.Context, key K, value *V, maxTTL time.Time) (SetResult[V], error) {
	m.initDo()
	shard := m.choose(key)
	m.mus[shard].Lock()
	wp, ok := m.maps[shard].Get(key)
	if ok {
		if live := wp.Value(); live != nil {
			m.mus[shard].Unlock()
			return SetResult[V]{Prev: live, Replaced: true}, nil
		}
	}
	newWP := weak.Make(value)
	stored := value
	var dedup bool
	if m.tree != nil {
		m.treeMu.Lock()
		newWP, dedup = m.treeAdd(newWP)
		if dedup {
			stored = storedRep(newWP, value)
		}
	}
	oldWP, hadBucket := m.maps[shard].Set(key, newWP, maxTTL)
	if m.tree != nil {
		if hadBucket {
			// Replacing a bucket whose weak pointer had gone nil: drop the reference to its old (now-collected)
			// representative so the tree entry is reclaimed once nothing else references it.
			m.treeRelease(oldWP)
		}
		m.treeMu.Unlock()
	}
	m.mus[shard].Unlock()
	// Only count a brand-new bucket. Replacing a bucket whose weak pointer had gone nil keeps the same key, which
	// is already reflected in the count.
	fresh := !hadBucket
	if fresh {
		m.count.Add(1)
	}
	// SetIfNil only stores over a collected or absent bucket, so the new object differs from any prior one; compute
	// StoredUnchanged for symmetry with Set (it is effectively always false on this path).
	storedUnchanged := hadBucket && oldWP == newWP
	return SetResult[V]{Stored: stored, Ok: true, Fresh: fresh, Deduped: dedup, StoredUnchanged: storedUnchanged}, nil
}

// Filler is a function that will be run when a value is missing from the cache. If this function
// returns an error, the value will not be set.
type Filler[K comparable, V any] func(ctx context.Context, k K) (value *V, ok bool, err error)

// Get returns a value for a key. It reports false when no value has been assigned for key. This is a pure read:
// filling a missing value is handled one layer up in the weak.Cache so the fill and its TTL/cleanup bookkeeping
// happen in a single place.
func (m *Map[K, V]) Get(key K) (value *V, ok bool) {
	m.initDo()
	shard := m.choose(key)
	m.mus[shard].RLock()
	wp, ok := m.maps[shard].Get(key)
	m.mus[shard].RUnlock()
	if !ok {
		return nil, false
	}
	value = wp.Value()
	if value == nil {
		return nil, false
	}
	return value, true
}

// Deleter is a function that will be run when deleting a value from the cache. If this function
// returns an error, the value will not be deleted.
type Deleter[K comparable] func(ctx context.Context, k K) error

// Delete deletes a value for a key. If deleter is non-nil it runs under the shard write lock before the in-memory
// delete, even when the key holds no live entry (a weak entry may already have been reclaimed while its durable
// copy remains). If the deleter returns an error the in-memory delete is aborted and the error is returned.
// prev is the deleted live value, or nil when the key was absent or its value had already been collected. removed
// reports whether a bucket was physically removed from the map (and therefore whether the count was decremented),
// which is true even when prev is nil because the bucket held an already-collected weak pointer. Callers use removed,
// not prev, to keep their own item counters symmetric with the map.
func (m *Map[K, V]) Delete(ctx context.Context, key K, deleter Deleter[K]) (prev *V, removed bool, err error) {
	m.initDo()
	shard := m.choose(key)
	m.mus[shard].Lock()
	if deleter != nil {
		if err := deleter(ctx, key); err != nil {
			m.mus[shard].Unlock()
			return nil, false, err
		}
	}

	wp, deleted := m.maps[shard].Delete(key)
	if !deleted {
		m.mus[shard].Unlock()
		return nil, false, nil
	}
	m.count.Add(-1)
	m.releaseRep(wp)
	m.mus[shard].Unlock()
	return wp.Value(), true, nil
}

// DeleteIfMaxTTL deletes a value for a key only if the current value's TTL is less than or equal to maxTTL. When
// deleter is nil the selection and removal are done in a single hashmap traversal (DeleteIfMaxTTL). When deleter is
// non-nil it runs under the shard write lock, and only when the TTL condition selected the entry for eviction:
// following Delete's ordering, the entry is peeked and the deleter run before the bucket is removed, so if the deleter
// returns an error nothing is mutated and the error is returned, letting the caller keep the entry in its expireAfter
// tree so the next tick retries.
func (m *Map[K, V]) DeleteIfMaxTTL(ctx context.Context, key K, maxTTL time.Time, deleter Deleter[K]) (prev *V, deleted bool, err error) {
	m.initDo()
	shard := m.choose(key)
	m.mus[shard].Lock()
	if deleter == nil {
		// No deleter: select and remove the bucket in one traversal. The returned weak pointer carries the refcount
		// and count bookkeeping that the peek-then-delete path would otherwise get from GetIfMaxTTL.
		wp, removed := m.maps[shard].DeleteIfMaxTTL(key, maxTTL)
		if !removed {
			m.mus[shard].Unlock()
			return nil, false, nil
		}
		m.count.Add(-1)
		m.releaseRep(wp)
		m.mus[shard].Unlock()
		return wp.Value(), true, nil
	}
	wp, ok := m.maps[shard].GetIfMaxTTL(key, maxTTL)
	if !ok {
		m.mus[shard].Unlock()
		return nil, false, nil
	}
	if err := deleter(ctx, key); err != nil {
		// The TTL condition selected the entry, but the deleter failed. Nothing has been mutated, so leave the
		// bucket in place; the caller keeps the entry in its expireAfter tree so the next tick retries.
		m.mus[shard].Unlock()
		return nil, false, err
	}
	// The peek confirmed the bucket matches the TTL condition and we still hold the shard lock, so a plain Delete
	// removes exactly that bucket.
	m.maps[shard].Delete(key)
	m.count.Add(-1)
	m.releaseRep(wp)
	m.mus[shard].Unlock()
	return wp.Value(), true, nil
}

// DeleteIfNil deletes a value for a key only if the current value's weak pointer is nil.
func (m *Map[K, V]) DeleteIfNil(key K) (prev *V, deleted bool) {
	m.initDo()
	shard := m.choose(key)
	m.mus[shard].Lock()

	// GetAny, not Get: this is the GC-reclaim liveness path, and a bucket whose value was collected can also be past
	// its maxTTL deadline. Get hides a past-maxTTL bucket (reporting the key absent), which would leave a poison
	// bucket (one a failing deleter keeps stranded) here forever: the deadline-only GetIfMaxTTL would keep re-running
	// the failing deleter every tick. GetAny reports the bucket regardless of maxTTL so the nil value below is seen
	// and the bucket removed, which then lets the next tick's GetIfMaxTTL miss and stop retrying the deleter.
	wp, ok := m.maps[shard].GetAny(key)
	if !ok {
		m.mus[shard].Unlock()
		return nil, false
	}
	val := wp.Value()
	if val != nil {
		m.mus[shard].Unlock()
		return nil, false
	}

	m.maps[shard].Delete(key)
	m.count.Add(-1)
	// wp.Value() is nil here, but refs is keyed by wp's pointer identity, so releaseRep releases the representative
	// exactly rather than by the comparator's least-ordering of nil-valued pointers.
	m.releaseRep(wp)
	m.mus[shard].Unlock()
	return val, true
}

// CleanShards removes all entries with nil values from the map.
func (m *Map[K, V]) CleanShards() {
	m.initDo()
	for shard := range m.maps {
		m.mus[shard].Lock()
		for k, v := range m.maps[shard].All() {
			if v.Value() == nil {
				if _, deleted := m.maps[shard].Delete(k); deleted {
					m.count.Add(-1)
					m.releaseRep(v)
				}
			}
		}
		m.mus[shard].Unlock()
	}
}

// Len returns the number of values in map. This is an approximation since keys may hold nil values that
// have not yet been cleaned up.
func (m *Map[K, V]) Len() int {
	m.initDo()
	return int(m.count.Load())
}

// all returns a sequence of all key/values. It is not safe to call
// Set or Delete while iterating.
func (m *Map[K, V]) all() iter.Seq2[K, *V] {
	m.initDo()
	return func(yield func(K, *V) bool) {
		for i := 0; i < m.shards; i++ {
			for k, wp := range m.maps[i].All() {
				v := wp.Value()
				if v == nil {
					continue
				}
				if !yield(k, v) {
					return
				}
			}
		}
	}
}

func (m *Map[K, V]) choose(key K) int {
	return int(maphash.Comparable(m.seed, key) & uint64(m.shards-1))
}

func (m *Map[K, V]) initDo() {
	m.init.Do(func() {
		m.shards = 1
		for m.shards < runtime.NumCPU()*16 {
			m.shards *= 2
		}
		scap := m.cap / m.shards
		m.mus = make([]sync.RWMutex, m.shards)
		m.maps = make([]*rhh.Map[K, weak.Pointer[V]], m.shards)
		for i := 0; i < len(m.maps); i++ {
			m.maps[i] = rhh.New[K, weak.Pointer[V]](scap)
		}
		m.seed = maphash.MakeSeed()
	})
}
