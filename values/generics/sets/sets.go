// Package sets provides a generic set type.
package sets

import (
	"cmp"
	"fmt"
	"slices"
)

// Set is a generic set type.
type Set[E cmp.Ordered] struct {
	m map[E]struct{}
}

func (s *Set[E]) init() {
	if s.m == nil {
		s.m = make(map[E]struct{})
	}
}

// Len returns the number of elements in the Set.
func (s *Set[E]) Len() int {
	return len(s.m)
}

// Add adds the given values to the Set.
func (s *Set[E]) Add(vals ...E) {
	s.init()
	for _, v := range vals {
		s.m[v] = struct{}{}
	}
}

// Remove removes the given value from the Set.
func (s *Set[E]) Remove(v E) {
	delete(s.m, v)
}

// Contains returns true if the Set contains the given value.
func (s *Set[E]) Contains(v E) bool {
	if s.m == nil {
		return false
	}
	_, ok := s.m[v]
	return ok
}

// Members returns all the members of the Set. This is a copy of the entries in the Set.
// This returned slice is sorted. This is a new slice and can be modified without affecting the Set.
func (s *Set[E]) Members() []E {
	if s.m == nil {
		return nil
	}

	result := make([]E, 0, len(s.m))
	for v := range s.m {
		result = append(result, v)
	}

	slices.Sort(result)
	return result
}

// String returns a string representation of the Set. This implements the fmt.Stringer interface.
func (s *Set[E]) String() string {
	return fmt.Sprintf("%v", s.Members())
}

// Union returns a new Set that is the union of the two Sets. This creates a new Set.
func (s *Set[E]) Union(s2 Set[E]) Set[E] {
	result := Set[E]{}
	result.Add(s.Members()...)
	result.Add(s2.Members()...)
	return result
}

// Intersection returns a new Set that is the intersection of the two Sets.
func (s *Set[E]) Intersection(s2 Set[E]) Set[E] {
	result := Set[E]{}
	for _, v := range s.Members() {
		if s2.Contains(v) {
			result.Add(v)
		}
	}
	return result
}
