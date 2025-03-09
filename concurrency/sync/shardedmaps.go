package sync

import (
	"github.com/gostdlib/base/concurrency/sync/internal/shardmap"
)

// ShardedMap is a map that is sharded by a hash of the key. This allows for multiple readers and writers to
// access the map without blocking as long as they are accessing different maps. This is useful for when you
// have a large number of keys and you want to reduce contention on the map. This map also shrinks with deleted
// keys, so it will not grow indefinitely like a standard map. This is faster than sync.Map in most cases
// (even in 1.24) and is more memory efficient.
type ShardedMap[K comparable, V any] struct {
	rw   *RWMutex
	once Once
	sm   shardmap.Map[K, V]
}

func (s *ShardedMap[K, V]) Len() int {
	s.once.Do(s.init)
	s.rw.RLock()
	defer s.rw.RUnlock()
	return s.sm.Len()
}

// Get returns the value for the given key. If ok is false, the key was not found.
func (s *ShardedMap[K, V]) Get(k K) (value V, ok bool) {
	s.once.Do(s.init)
	s.rw.RLock()
	defer s.rw.RUnlock()
	return s.sm.Get(k)
}

// Set sets the value for the given key. This will return the previous value and if the key existed.
// If ok is false, prev is the zero value for the value type.
func (s *ShardedMap[K, V]) Set(k K, v V) (prev V, ok bool) {
	s.once.Do(s.init)
	s.rw.RLock()
	defer s.rw.RUnlock()
	return s.sm.Set(k, v)
}

// Del deletes the value for the given key. It returns the previous value and if the key existed.
// If ok is false, prev is the zero value for the value type.
func (s *ShardedMap[K, V]) Del(k K) (prev V, ok bool) {
	s.once.Do(s.init)
	s.rw.RLock()
	defer s.rw.RUnlock()
	return s.sm.Delete(k)
}

// Map will take all maps and merge them into a single map. This is a blocking operation that must lock all maps.
func (s *ShardedMap[K, V]) Map() map[K]V {
	s.once.Do(s.init)
	s.rw.Lock()
	defer s.rw.Unlock()

	m := make(map[K]V, s.sm.Len())
	for k, v := range s.sm.All() {
		m[k] = v
	}
	return m
}

func (s *ShardedMap[K, V]) init() {
	s.rw = &RWMutex{}
}
