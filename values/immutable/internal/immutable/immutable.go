// Package immutable provides some immutable types for slices and maps. It exists in this
// package to only expose the UnsafeMap and UnsafeSlice functions via the unsafe package.
// The top level immutable package uses type aliases to gain acccess to the Map and Slice.
//
// This package is waiting on 1.24 to be released which has generic type aliases.
package immutable

import (
	"iter"
	"maps"
	"slices"
)

// Map provides a read-only map as long as the values are not pointers or references.
type Map[K comparable, V any] struct {
	m map[K]V
}

// NewMap returns a new immutable map.
func NewMap[K comparable, V any](m map[K]V) Map[K, V] {
	return Map[K, V]{m: m}
}

// Copy returns a copy of the underlying map.
func (m Map[K, V]) Copy() map[K]V {
	return CopyMap(m.m)
}

// Get returns the value for the given key.
func (m Map[K, V]) Get(k K) (value V, ok bool) {
	v, ok := m.m[k]
	return v, ok
}

// All returns an iterator over the map.
func (m Map[K, V]) All() iter.Seq2[K, V] {
	return maps.All(m.m)
}

// unsafeMap returns the underlying map. This is unsafe because it allows the caller to modify the map.
func UnsafeMap[K comparable, V any](m Map[K, V]) map[K]V {
	return m.m
}

// Slice provides a read-only slice as long as the values are not pointers or references.
type Slice[T any] struct {
	s []T
}

// NewSlice returns a new immutable slice.
func NewSlice[T any](s []T) Slice[T] {
	return Slice[T]{s: s}
}

// Copy returns a copy of the underlying slice.
func (s Slice[T]) Copy() []T {
	return CopySlice(s.s)
}

// Get returns the value at the given index. This will panic if the index is out of range.
func (s Slice[T]) Get(i int) T {
	return s.s[i]
}

// Len returns the length of the slice.
func (s Slice[T]) Len() int {
	return len(s.s)
}

// All returns an iterator over the slice.
func (s Slice[T]) All() iter.Seq2[int, T] {
	return slices.All(s.s)
}

// UnsafeSlice returns the underlying slice. This is unsafe because it allows the caller to modify the slice.
func UnsafeSlice[T any](s Slice[T]) []T {
	return s.s
}

// CopyMap returns a copy of the given map. This is useful for creating an immutable map.
func CopyMap[K comparable, V any](m map[K]V) map[K]V {
	n := make(map[K]V, len(m))
	for k, v := range m {
		n[k] = v
	}
	return n
}

// CopySlice returns a copy of the given slice. This is useful for creating an immutable slice.
func CopySlice[T any](s []T) []T {
	n := make([]T, len(s))
	copy(n, s)
	return n
}
