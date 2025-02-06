package sync

import (
	"fmt"
	"hash/maphash"
	"runtime"
	"sync"
)

// ShardedMap is a map that is sharded by a hash of the key. This allows for multiple readers and writers to
// access the map without blocking as long as they are accessing different maps. This is useful for when you
// have a large number of keys and you want to reduce contention on the map. There is no iteration over the map
// because there is no good way to do this without locking all maps and worrying about deadlocks.
type ShardedMap[K comparable, V any] struct {
	// N is the number of maps in the ShardedMap. If N is 0, it will be set to runtime.NumCPU().
	N int

	locks []*sync.RWMutex
	maps  []map[K]V
	seed  maphash.Seed

	once sync.Once
}

func (s *ShardedMap[K, V]) init() {
	s.once.Do(s.setup)
}

func (s *ShardedMap[K, V]) setup() {
	n := s.N
	if n < 1 {
		n = runtime.NumCPU()
	}
	locks := make([]*sync.RWMutex, n)
	var maps []map[K]V
	for i := range locks {
		locks[i] = &sync.RWMutex{}
		maps = append(maps, make(map[K]V))
	}

	s.locks = locks
	s.maps = maps
	s.seed = maphash.MakeSeed()
}

func (s *ShardedMap[K, V]) getShard(k K) int {
	s.init() // Causes setup to be called only once.
	var h maphash.Hash
	h.SetSeed(s.seed)
	h.WriteString(fmt.Sprintf("%v", k)) // Even unsafe methods don't beat fmt.Sprintf here.
	return int(h.Sum64() % uint64(len(s.locks)))
}

// Get returns the value for the given key. If ok is false, the key was not found.
func (s *ShardedMap[K, V]) Get(k K) (value V, ok bool) {
	i := s.getShard(k)
	s.locks[i].RLock()
	v, ok := s.maps[i][k]
	s.locks[i].RUnlock()
	return v, ok
}

// Set sets the value for the given key. This will return the previous value and if the key existed.
// If ok is false, prev is the zero value for the value type.
func (s *ShardedMap[K, V]) Set(k K, v V) (prev V, ok bool) {
	i := s.getShard(k)
	s.locks[i].Lock()
	prev, ok = s.maps[i][k]
	s.maps[i][k] = v
	s.locks[i].Unlock()
	return prev, ok
}

// Del deletes the value for the given key. It returns the previous value and if the key existed.
// If ok is false, prev is the zero value for the value type.
func (s *ShardedMap[K, V]) Del(k K) (prev V, ok bool) {
	i := s.getShard(k)
	s.locks[i].Lock()
	prev, ok = s.maps[i][k]
	delete(s.maps[i], k)
	s.locks[i].Unlock()
	return prev, ok
}

// Map will take all maps and merge them into a single map. This is a blocking operation that must lock all maps.
func (s *ShardedMap[K, V]) Map() map[K]V {
	size := 0
	for i, l := range s.locks {
		l.RLock()
		size += len(s.maps[i])
	}
	defer func() {
		for _, l := range s.locks {
			l.RUnlock()
		}
	}()

	nMap := make(map[K]V, size)
	for _, m := range s.maps {
		for k, v := range m {
			nMap[k] = v
		}
	}
	return nMap
}
