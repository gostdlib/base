// Package sets provides a generic set type.
package sets

import (
	"fmt"

	"github.com/gostdlib/base/concurrency/sync"
)

// Set is a generic set type.
type Set[E comparable] struct {
	m sync.ShardedMap[E, struct{}]
}

// Len returns the number of elements in the Set.
func (s *Set[E]) Len() int {
	return s.m.Len()
}

// Add adds the given values to the Set. Thread safe.
func (s *Set[E]) Add(vals ...E) {
	for _, v := range vals {
		s.m.Set(v, struct{}{})
	}
	return
}

// Remove removes the given value from the Set. If the value is not in the Set, this is a no-op. Thread safe.
func (s *Set[E]) Remove(v E) {
	s.m.Del(v)
}

// Contains returns true if the Set contains the given value. Thread safe.
func (s *Set[E]) Contains(v E) bool {
	_, ok := s.m.Get(v)
	return ok
}

// Members returns all the members of the Set. This is a copy of the entries in the Set.
// This returned slice is has random order. This is a new slice and can be modified without affecting the Set, but modifying
// the elements themselves will affect the Set if they are reference types.
// Not thread safe.
func (s *Set[E]) Members() []E {
	if s.m.Len() == 0 {
		return nil
	}

	result := make([]E, 0, s.m.Len())
	for k, _ := range s.m.All() {
		result = append(result, k)
	}

	return result
}

// String returns a string representation of the Set. This implements the fmt.Stringer interface.
// Not thread safe.
func (s *Set[E]) String() string {
	return fmt.Sprintf("%v", s.Members())
}

// Union returns a new Set that is the union of the two Sets. This creates a new Set.
func (s *Set[E]) Union(s2 *Set[E]) Set[E] {
	result := Set[E]{}
	result.Add(s.Members()...)
	result.Add(s2.Members()...)
	return result // It is okay that this copies a lock value, because it is unused.
}

// Intersection returns a new Set that is the intersection of the two Sets.
// This is not thread safe.
func (s *Set[E]) Intersection(s2 *Set[E]) Set[E] {
	result := Set[E]{}
	for _, v := range s.Members() {
		if s2.Contains(v) {
			result.Add(v)
		}
	}
	return result // It is okay that this copies a lock value, because it is unused.
}
