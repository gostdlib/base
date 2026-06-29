package sync

import (
	"iter"
	"sync"

	"github.com/gostdlib/base/concurrency/sync/internal/shardmap"
)

// ShardedMap is a map that is sharded by a hash of the key. This allows for multiple readers and writers to
// access the map without blocking as long as they are accessing different maps. This is useful for when you
// have a large number of keys and you want to reduce contention on the map. This map also shrinks with deleted
// keys, so it will not grow indefinitely like a standard map. This is faster than sync.Map in most cases
// (even in 1.24) and is more memory efficient. This is thread safe, unless you are using the All() method, which
// is not thread safe.
type ShardedMap[K comparable, V any] struct {
	// IsEqual is a function that is used to compare two values for equality. This is not required
	// unless using CompareAndSwap or CompareAndDelete.
	IsEqual func(a, b V) bool
	once    sync.Once
	sm      shardmap.Map[K, V]
}

// Len returns the number of keys in the map.
func (s *ShardedMap[K, V]) Len() int {
	return s.sm.Len()
}

// Get returns the value for the given key. If ok is false, the key was not found.
func (s *ShardedMap[K, V]) Get(k K) (value V, ok bool) {
	return s.sm.Get(k)
}

// Set sets the value for the given key. This will return the previous value and if the key existed.
// If ok is false, prev is the zero value for the value type.
func (s *ShardedMap[K, V]) Set(k K, v V) (prev V, ok bool) {
	return s.sm.Set(k, v)
}

// CompareAndSwap sets the value for the given key if the current value is equal to the old value.
// If the key does not exist but the old value is the zero value for the type, this will create the value
// and set it to new. This will return true if the value was set, false otherwise.
func (s *ShardedMap[K, V]) CompareAndSwap(k K, old, new V) (swapped bool) {
	s.once.Do(func() {
		if s.sm.IsEqual == nil {
			s.sm.IsEqual = s.IsEqual
		}
	})
	return s.sm.CompareAndSwap(k, old, new)
}

// Del deletes the value for the given key. It returns the previous value and if the key existed.
// If ok is false, prev is the zero value for the value type.
func (s *ShardedMap[K, V]) Del(k K) (prev V, ok bool) {
	return s.sm.Delete(k)
}

// CompareAndDelete deletes the value for the given key if the current value is equal to the old value.
// If the key does not exist, this returns true.
func (s *ShardedMap[K, V]) CompareAndDelete(k K, old V) (deleted bool) {
	s.once.Do(func() {
		if s.sm.IsEqual == nil {
			s.sm.IsEqual = s.IsEqual
		}
	})
	return s.sm.CompareAndDelete(k, old)
}

// SetAccept assigns a value to a key. The "accept" function can be used to inspect the previous value, if any,
// and accept or reject the change. It also provides a safe way to block other goroutines from writing to the
// same shard while inspecting. Returns the previous value, or false when no value was assigned.
func (s *ShardedMap[K, V]) SetAccept(key K, value V, accept func(prev V, replaced bool) bool) (prev V, replaced bool) {
	s.once.Do(func() {
		if s.sm.IsEqual == nil {
			s.sm.IsEqual = s.IsEqual
		}
	})
	return s.sm.SetAccept(key, value, accept)
}

// DeleteAccept deletes a value for a key. The "accept" function can be used to inspect the previous value,
// if any, and accept or reject the change. It also provides a safe way to block other goroutines from writing to the
// same shard while inspecting. Returns the deleted value, or false when no value was assigned.
func (s *ShardedMap[K, V]) DeleteAccept(key K, accept func(prev V, deleted bool) bool) (prev V, deleted bool) {
	s.once.Do(func() {
		if s.sm.IsEqual == nil {
			s.sm.IsEqual = s.IsEqual
		}
	})
	return s.sm.DeleteAccept(key, accept)
}

type allOptions struct {
	lock bool
}

type AllOption func(o allOptions) allOptions

// WithLock makes All() read-lock each shard while yielding that shard's entries, so other goroutines may
// safely read and write the map while you iterate. Locking is per-shard and released as each shard
// completes, so iteration is not a whole-map snapshot: writes to shards other than the one being yielded
// happen concurrently and may or may not be observed. Do not call Get(), Set(), or Del() from inside the
// iteration loop.
func WithLock() AllOption {
	return func(o allOptions) allOptions {
		o.lock = true
		return o
	}
}

// All returns an iterator for the map for use with range. Without options it is not thread safe: only use
// it when no other reads or writes are happening. Pass WithLock() to make iteration safe alongside
// concurrent readers and writers; see WithLock for the per-shard semantics and restrictions.
func (s *ShardedMap[K, V]) All(options ...AllOption) iter.Seq2[K, V] {
	if len(options) == 0 {
		return s.sm.All()
	}
	var opts allOptions
	for _, option := range options {
		opts = option(opts)
	}
	if opts.lock {
		return s.sm.AllLocked()
	}
	return s.sm.All()
}
