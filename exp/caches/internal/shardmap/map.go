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
	"weak"

	"github.com/gostdlib/base/exp/caches/internal/btree"
	rhh "github.com/gostdlib/base/exp/caches/internal/hashmap"
)

// Map is a hashmap. Like map[string]interface{}, but sharded and thread-safe.
type Map[K comparable, V any] struct {
	// IsEqual is a function that determines if two values are equal. This is not required unless using
	// CompareAndSwap or CompareAndDelete.
	IsEqual func(old, new V) bool
	init    sync.Once
	cap     int
	shards  int
	mus     []sync.RWMutex
	maps    []*rhh.Map[K, weak.Pointer[V]]

	tree *btree.BTreeG[weak.Pointer[V]]
	less func(a, b weak.Pointer[V]) bool

	count atomic.Int64

	seed maphash.Seed
}

// New returns a new hashmap with the specified capacity. This function is only
// needed when you must define a minimum capacity, otherwise just use:
//
//	var m shardmap.Map
func New[K comparable, V any](less func(a, b weak.Pointer[V]) bool) *Map[K, V] {
	var tree *btree.BTreeG[weak.Pointer[V]]
	if less != nil {
		tree = btree.NewBTreeG[weak.Pointer[V]](less)
	}

	m := &Map[K, V]{tree: tree, less: less}
	m.initDo()
	return m
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
		m.tree.Clear()
	}
}

// Setter is a function that will be run when setting a value in the cache. If this function
// returns an error, the value will not be set.
type Setter[K comparable, V any] func(ctx context.Context, k K, v *V) error

// Set assigns a value to a key.
// Returns the previous value, or false when no value was assigned.
func (m *Map[K, V]) Set(ctx context.Context, key K, value *V, setter Setter[K, V]) (prev *V, replaced bool, err error) {
	m.initDo()

	shard := m.choose(key)
	m.mus[shard].Lock()
	if setter != nil {
		if err := setter(ctx, key, value); err != nil {
			m.mus[shard].Unlock()
			return nil, false, err
		}
	}
	wp := weak.Make(value)
	if m.tree != nil {
		wp, _ = m.tree.Set(wp)
	}
	wp, replaced = m.maps[shard].Set(key, wp)
	m.mus[shard].Unlock()
	prev = wp.Value()
	if replaced && prev != nil {
		return prev, replaced, nil
	}
	m.count.Add(1)
	return prev, false, nil
}

// SetIfNil assigns a value to a key only if the current value's weak pointer is nil or the key does not exist.
func (m *Map[K, V]) SetIfNil(ctx context.Context, key K, value *V) (setOk bool, err error) {
	m.initDo()
	shard := m.choose(key)
	m.mus[shard].Lock()
	wp, ok := m.maps[shard].Get(key)
	if ok {
		existing := wp.Value()
		if existing != nil {
			m.mus[shard].Unlock()
			return false, nil
		}
	}
	m.maps[shard].Set(key, weak.Make(value))
	m.mus[shard].Unlock()
	m.count.Add(1)
	return true, nil
}

// Filler is a function that will be run when a value is missing from the cache. If this function
// returns an error, the value will not be set.
type Filler[K comparable, V any] func(ctx context.Context, k K) (value *V, ok bool, err error)

// Get returns a value for a key.
// Returns false when no value has been assign for key.
func (m *Map[K, V]) Get(ctx context.Context, key K, filler Filler[K, V]) (value *V, ok bool, err error) {
	m.initDo()
	shard := m.choose(key)
	m.mus[shard].RLock()
	wp, ok := m.maps[shard].Get(key)
	m.mus[shard].RUnlock()
	if !ok {
		if filler == nil {
			return nil, false, nil
		}
		v, ok, err := filler(ctx, key)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, nil
		}
		setOk, err := m.SetIfNil(ctx, key, v)
		if err != nil {
			return nil, false, err
		}
		if !setOk {
			return m.Get(ctx, key, filler)
		}

		return v, true, nil
	}
	value = wp.Value()
	if value == nil {
		return nil, false, nil
	}
	return value, ok, nil
}

// Deleter is a function that will be run when deleting a value from the cache. If this function
// returns an error, the value will not be deleted.
type Deleter[K comparable] func(ctx context.Context, k K) error

// Delete deletes a value for a key.
// Returns the deleted value, or false when no value was assigned.
func (m *Map[K, V]) Delete(ctx context.Context, key K, deleter Deleter[K]) (prev *V, deleted bool, err error) {
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
		return nil, deleted, nil
	}
	m.count.Add(-1)
	prev = wp.Value()
	if prev == nil {
		m.mus[shard].Unlock()
		return nil, false, nil
	}
	if m.tree != nil {
		m.tree.Delete(wp)
	}
	m.mus[shard].Unlock()
	return prev, deleted, nil
}

// DeleteIfNil deletes a value for a key only if the current value's weak pointer is nil.
func (m *Map[K, V]) DeleteIfNil(key K) (prev *V, deleted bool) {
	m.initDo()
	shard := m.choose(key)
	m.mus[shard].Lock()

	wp, ok := m.maps[shard].Get(key)
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
	if m.tree != nil {
		m.tree.Delete(wp)
	}
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
