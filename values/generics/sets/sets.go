// Package sets provides a generic set type.
package sets

import (
	"cmp"
	"fmt"
	"slices"
)

// Set is a generic set type.
type Set[E cmp.Ordered] map[E]struct{}

// New creates a new Set with the given values.
func New[E cmp.Ordered](vals ...E) Set[E] {
	s := Set[E]{}
	for _, v := range vals {
		s[v] = struct{}{}
	}
	return s
}

// Add adds the given values to the Set.
func (s Set[E]) Add(vals ...E) {
	for _, v := range vals {
		s[v] = struct{}{}
	}
}

// Remove removes the given value from the Set.
func (s Set[E]) Remove(v E) {
	delete(s, v)
}

// Contains returns true if the Set contains the given value.
func (s Set[E]) Contains(v E) bool {
	_, ok := s[v]
	return ok
}

// Members returns all the members of the Set.
func (s Set[E]) Members() []E {
	result := make([]E, 0, len(s))
	for v := range s {
		result = append(result, v)
	}

	slices.Sort(result)
	return result
}

// String returns a string representation of the Set. This implements the fmt.Stringer interface.
func (s Set[E]) String() string {
	return fmt.Sprintf("%v", s.Members())
}

// Union returns a new Set that is the union of the two Sets.
func (s Set[E]) Union(s2 Set[E]) Set[E] {
	result := New(s.Members()...)
	result.Add(s2.Members()...)
	return result
}

// Intersection returns a new Set that is the intersection of the two Sets.
func (s Set[E]) Intersection(s2 Set[E]) Set[E] {
	result := New[E]()
	for _, v := range s.Members() {
		if s2.Contains(v) {
			result.Add(v)
		}
	}
	return result
}
