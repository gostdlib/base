package hashmap

import (
	"hash/maphash"
	"iter"
	"time"
)

const (
	loadFactor  = 0.85                      // must be above 50%
	dibBitSize  = 16                        // 0xFFFF
	hashBitSize = 64 - dibBitSize           // 0xFFFFFFFFFFFF
	maxHash     = ^uint64(0) >> dibBitSize  // max 28,147,497,671,0655
	maxDIB      = ^uint64(0) >> hashBitSize // max 65,535
)

var seed = maphash.MakeSeed()

type entry[K comparable, V any] struct {
	hdib   uint64 // bitfield { hash:48 dib:16 }
	value  V      // user value
	key    K      // user key
	maxTTL time.Time
}

func (e *entry[K, V]) dib() int {
	return int(e.hdib & maxDIB)
}
func (e *entry[K, V]) hash() int {
	return int(e.hdib >> dibBitSize)
}
func (e *entry[K, V]) setDIB(dib int) {
	e.hdib = e.hdib>>dibBitSize<<dibBitSize | uint64(dib)&maxDIB
}
func (e *entry[K, V]) setHash(hash int) {
	e.hdib = uint64(hash)<<dibBitSize | e.hdib&maxDIB
}
func makeHDIB(hash, dib int) uint64 {
	return uint64(hash)<<dibBitSize | uint64(dib)&maxDIB
}

// hash returns a 48-bit hash for 64-bit environments, or 32-bit hash for
// 32-bit environments.
func (m *Map[K, V]) hash(key K) int {
	return int(maphash.Comparable(seed, key) >> dibBitSize)
}

// Map is a hashmap. Like map[string]interface{}
type Map[K comparable, V any] struct {
	cap      int
	length   int
	mask     int
	growAt   int
	shrinkAt int
	buckets  []entry[K, V]
}

// New returns a new Map. Like map[string]interface{}
func New[K comparable, V any](cap int) *Map[K, V] {
	m := new(Map[K, V])
	m.cap = cap
	sz := 8
	for sz < m.cap {
		sz *= 2
	}
	if m.cap > 0 {
		m.cap = sz
	}
	m.buckets = make([]entry[K, V], sz)
	m.mask = len(m.buckets) - 1
	m.growAt = int(float64(len(m.buckets)) * loadFactor)
	m.shrinkAt = int(float64(len(m.buckets)) * (1 - loadFactor))
	return m
}

func (m *Map[K, V]) resize(newCap int) {
	nmap := New[K, V](newCap)
	for i := 0; i < len(m.buckets); i++ {
		if m.buckets[i].dib() > 0 {
			nmap.set(m.buckets[i].hash(), m.buckets[i].key, m.buckets[i].value, m.buckets[i].maxTTL)
		}
	}
	cap := m.cap
	*m = *nmap
	m.cap = cap
}

// Set assigns a value to a key.
// Returns the previous value, or false when no value was assigned.
func (m *Map[K, V]) Set(key K, value V, maxTTL time.Time) (V, bool) {
	if len(m.buckets) == 0 {
		*m = *New[K, V](0)
	}
	if m.length >= m.growAt {
		m.resize(len(m.buckets) * 2)
	}
	return m.set(m.hash(key), key, value, maxTTL)
}

func (m *Map[K, V]) set(hash int, key K, value V, maxTTL time.Time) (prev V, ok bool) {
	e := entry[K, V]{makeHDIB(hash, 1), value, key, maxTTL}
	i := e.hash() & m.mask
	for {
		if m.buckets[i].dib() == 0 {
			m.buckets[i] = e
			m.length++
			return prev, false
		}
		if e.hash() == m.buckets[i].hash() && e.key == m.buckets[i].key {
			prev = m.buckets[i].value
			m.buckets[i].value = e.value
			m.buckets[i].maxTTL = e.maxTTL
			return prev, true
		}
		if m.buckets[i].dib() < e.dib() {
			e, m.buckets[i] = m.buckets[i], e
		}
		i = (i + 1) & m.mask
		e.setDIB(e.dib() + 1)
	}
}

// Get returns a value for a key.
// Returns false when no value has been assign for key.
func (m *Map[K, V]) Get(key K) (value V, ok bool) {
	if len(m.buckets) == 0 {
		return value, false
	}
	hash := m.hash(key)
	i := hash & m.mask
	for {
		if m.buckets[i].dib() == 0 {
			return value, false
		}
		if m.buckets[i].hash() == hash && m.buckets[i].key == key {
			// An entry past its maxTTL is treated as a plain miss without mutating the map. Get is called under a
			// read lock, so it must not delete. The bucket is reclaimed later by DeleteIfMaxTTL (the forced maxTTL
			// eviction) or by Delete/DeleteIfNil once the value is GC'd. The weak.Cache registers every entry it
			// stores, whether written through Set or loaded through a filler, so both removal paths reach them.
			if !m.buckets[i].maxTTL.IsZero() && time.Now().After(m.buckets[i].maxTTL) {
				return value, false
			}
			return m.buckets[i].value, true
		}
		i = (i + 1) & m.mask
	}
}

// GetAny returns a value for a key without applying the maxTTL expiry filter that Get uses. Get treats a bucket past
// its maxTTL deadline as a miss so read paths do not observe an entry due for forced eviction; GetAny instead reports
// the bucket whenever it physically exists. A liveness-only caller (such as the GC-reclaim DeleteIfNil path) needs this
// raw view: hiding a past-maxTTL bucket the way Get does would make that caller believe the key is absent and leave the
// bucket (and everything scheduled against it) stranded rather than removing it once its weak value has been collected.
func (m *Map[K, V]) GetAny(key K) (value V, ok bool) {
	if len(m.buckets) == 0 {
		return value, false
	}
	hash := m.hash(key)
	i := hash & m.mask
	for {
		if m.buckets[i].dib() == 0 {
			return value, false
		}
		if m.buckets[i].hash() == hash && m.buckets[i].key == key {
			return m.buckets[i].value, true
		}
		i = (i + 1) & m.mask
	}
}

// Len returns the number of values in map.
func (m *Map[K, V]) Len() int {
	return m.length
}

// Delete deletes a value for a key.
// Returns the deleted value, or false when no value was assigned.
func (m *Map[K, V]) Delete(key K) (prev V, deleted bool) {
	if len(m.buckets) == 0 {
		return prev, false
	}
	hash := m.hash(key)
	i := hash & m.mask
	for {
		if m.buckets[i].dib() == 0 {
			return prev, false
		}
		if m.buckets[i].hash() == hash && m.buckets[i].key == key {
			prev = m.buckets[i].value
			m.remove(i)
			return prev, true
		}
		i = (i + 1) & m.mask
	}
}

// DeleteIfMaxTTL deletes a value for a key only if the stored maxTTL deadline is set (non-zero) and is not after
// the passed maxTTL (i.e. storedDeadline <= maxTTL). An entry that was re-Set with a later deadline than the one
// requested for eviction is therefore skipped, so a refreshed entry is not incorrectly deleted.
// Returns the deleted value, or false when no value was assigned or the stored deadline should be kept.
func (m *Map[K, V]) DeleteIfMaxTTL(key K, maxTTL time.Time) (prev V, deleted bool) {
	if len(m.buckets) == 0 {
		return prev, false
	}
	hash := m.hash(key)
	i := hash & m.mask
	for {
		if m.buckets[i].dib() == 0 {
			return prev, false
		}
		if m.buckets[i].hash() == hash && m.buckets[i].key == key {
			stored := m.buckets[i].maxTTL
			if stored.IsZero() || stored.After(maxTTL) {
				return prev, false
			}
			// We delete when stored <= maxTTL. In production per-key deadlines only ever increase (each re-Set
			// pushes the deadline later), so the stored deadline equal to the requested one is the expected case;
			// a stored deadline strictly before the requested one cannot arise there and this branch just handles
			// it defensively rather than leaving a stale entry.
			prev = m.buckets[i].value
			m.remove(i)
			return prev, true
		}
		i = (i + 1) & m.mask
	}
}

// GetIfMaxTTL returns the value for key only if the stored maxTTL deadline is set (non-zero) and is not after the
// passed maxTTL (storedDeadline <= maxTTL), without removing the entry. It mirrors DeleteIfMaxTTL's selection
// condition so a caller can peek before running side effects and then delete. Returns false when the key is absent or
// the stored deadline should be kept.
func (m *Map[K, V]) GetIfMaxTTL(key K, maxTTL time.Time) (value V, ok bool) {
	if len(m.buckets) == 0 {
		return value, false
	}
	hash := m.hash(key)
	i := hash & m.mask
	for {
		if m.buckets[i].dib() == 0 {
			return value, false
		}
		if m.buckets[i].hash() == hash && m.buckets[i].key == key {
			stored := m.buckets[i].maxTTL
			if stored.IsZero() || stored.After(maxTTL) {
				return value, false
			}
			return m.buckets[i].value, true
		}
		i = (i + 1) & m.mask
	}
}

func (m *Map[K, V]) remove(i int) {
	m.buckets[i].setDIB(0)
	for {
		pi := i
		i = (i + 1) & m.mask
		if m.buckets[i].dib() <= 1 {
			m.buckets[pi] = entry[K, V]{}
			break
		}
		m.buckets[pi] = m.buckets[i]
		m.buckets[pi].setDIB(m.buckets[pi].dib() - 1)
	}
	m.length--
	if len(m.buckets) > m.cap && m.length <= m.shrinkAt {
		m.resize(m.length)
	}
}

// All returns an iterator over all key/values. This is a replacement for Scan using the
// new iter package. It's not safe to call or Set or Delete while scanning.
func (m *Map[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for i := 0; i < len(m.buckets); i++ {
			if m.buckets[i].dib() > 0 {
				if !yield(m.buckets[i].key, m.buckets[i].value) {
					return
				}
			}
		}
	}
}

// Keys returns all keys as a slice
func (m *Map[K, V]) Keys() []K {
	keys := make([]K, 0, m.length)
	for i := 0; i < len(m.buckets); i++ {
		if m.buckets[i].dib() > 0 {
			keys = append(keys, m.buckets[i].key)
		}
	}
	return keys
}

// Values returns all values as a slice
func (m *Map[K, V]) Values() []V {
	values := make([]V, 0, m.length)
	for i := 0; i < len(m.buckets); i++ {
		if m.buckets[i].dib() > 0 {
			values = append(values, m.buckets[i].value)
		}
	}
	return values
}

// Copy the hashmap.
func (m *Map[K, V]) Copy() *Map[K, V] {
	m2 := new(Map[K, V])
	*m2 = *m
	m2.buckets = make([]entry[K, V], len(m.buckets))
	copy(m2.buckets, m.buckets)
	return m2
}

// GetPos gets a single keys/value nearby a position.
// The pos param can be any valid uint64. Useful for grabbing a random item
// from the map.
func (m *Map[K, V]) GetPos(pos uint64) (key K, value V, ok bool) {
	for i := 0; i < len(m.buckets); i++ {
		index := (pos + uint64(i)) & uint64(m.mask)
		if m.buckets[index].dib() > 0 {
			return m.buckets[index].key, m.buckets[index].value, true
		}
	}
	// Empty map
	return key, value, false
}
