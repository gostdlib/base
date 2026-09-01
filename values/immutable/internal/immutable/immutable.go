// Package immutable provides some immutable types for slices and maps. It exists in this
// package to only expose the UnsafeMap and UnsafeSlice functions via the unsafe package.
// The top level immutable package uses type aliases to gain acccess to the Map and Slice.
package immutable

import (
	"fmt"
	"iter"
	"maps"
	"reflect"
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

// Len returns the length of the map.
func (m Map[K, V]) Len() int {
	return len(m.m)
}

// All returns an iterator over the map.
func (m Map[K, V]) All() iter.Seq2[K, V] {
	return maps.All(m.m)
}

// UnsafeMap returns the underlying map. This is unsafe because it allows the caller to modify the map.
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

// unsafeSlice returns the underlying slice. This is unsafe because it allows the caller to modify the slice.
func UnsafeSlice[T any](s Slice[T]) []T {
	return s.s
}

// Set provides a read-only set as long as the values are not pointers or references.
type Set[T comparable] struct {
	m map[T]struct{}
}

// NewSet returns a new immutable set built from the values in s. Duplicate values are collapsed.
func NewSet[T comparable](s []T) Set[T] {
	m := make(map[T]struct{}, len(s))
	for _, v := range s {
		m[v] = struct{}{}
	}
	return Set[T]{m: m}
}

// Len returns the number of elements in the Set.
func (s Set[T]) Len() int {
	return len(s.m)
}

// Contains returns true if the Set contains the given value.
func (s Set[T]) Contains(v T) bool {
	_, ok := s.m[v]
	return ok
}

// All returns an iterator over the members of the Set. Order is random.
func (s Set[T]) All() iter.Seq[T] {
	return maps.Keys(s.m)
}

// Members returns all the members of the Set in random order. This is a new slice and can be modified
// without affecting the Set, but modifying the elements themselves will affect the Set if they are
// reference types.
func (s Set[T]) Members() []T {
	if len(s.m) == 0 {
		return nil
	}
	return slices.Collect(maps.Keys(s.m))
}

// String returns a string representation of the Set. This implements the fmt.Stringer interface.
func (s Set[T]) String() string {
	return fmt.Sprintf("%v", s.Members())
}

// Union returns a new Set that is the union of the two Sets.
func (s Set[T]) Union(s2 Set[T]) Set[T] {
	m := make(map[T]struct{}, len(s.m)+len(s2.m))
	for k := range s.m {
		m[k] = struct{}{}
	}
	for k := range s2.m {
		m[k] = struct{}{}
	}
	return Set[T]{m: m}
}

// Intersection returns a new Set that is the intersection of the two Sets.
func (s Set[T]) Intersection(s2 Set[T]) Set[T] {
	small, large := s.m, s2.m
	if len(large) < len(small) {
		small, large = large, small
	}
	m := make(map[T]struct{}, len(small))
	for k := range small {
		if _, ok := large[k]; ok {
			m[k] = struct{}{}
		}
	}
	return Set[T]{m: m}
}

// Copier is an interface that allows a type to be copied. This is useful when the value stored
// in the immutable type is a pointer or reference. This allows a deep copy to be made if the
// type implements this interface.
type Copier[T any] interface {
	// Copy returns a copy of the value.
	Copy() T
}

// CopySlice returns a copy of the given slice. If T implements the Copier interface, each element is copied with
// its Copy method. Otherwise the elements are copied as they are, which for a pointer, map, slice or other
// reference type means the copy and the original still share what the element points at.
func CopySlice[T any](s []T) []T {
	n := make([]T, len(s))
	copies, nilable := copyTraits[T]()
	if !copies {
		copy(n, s)
		return n
	}
	for i, v := range s {
		n[i] = copyValue(v, nilable)
	}
	return n
}

// CopyMap returns a copy of the given map. If V implements the Copier interface, each value is copied with its
// Copy method. Otherwise the values are copied as they are, which for a pointer, map, slice or other reference
// type means the copy and the original still share what the value points at.
func CopyMap[K comparable, V any](m map[K]V) map[K]V {
	n := make(map[K]V, len(m))
	copies, nilable := copyTraits[V]()
	if !copies {
		maps.Copy(n, m)
		return n
	}
	for k, v := range m {
		n[k] = copyValue(v, nilable)
	}
	return n
}

// copyTraits reports whether values of type T may implement Copier, and so have to be examined one at a time,
// and whether they can be a nil pointer, which copyValue must then check for. Both are properties of T, so they
// are settled once per call rather than once per element: for a concrete T the whole container can be copied in
// bulk when copies is no, and only a pointer or interface T ever needs the nil check. The Copier probe boxes T's
// zero value instead of using reflection because a nil boxed zero is exactly the interface case (an interface T
// can hold anything, so its values must be asked individually), and for every other T the assertion asks T's own
// method set, which is the same question the per-element assertion would ask.
func copyTraits[T any]() (copies, nilable bool) {
	var z T
	a := any(z)
	if a == nil {
		return true, true
	}
	if _, ok := a.(Copier[T]); !ok {
		return false, false
	}
	switch reflect.TypeFor[T]().Kind() {
	case reflect.Pointer, reflect.UnsafePointer:
		return true, true
	}
	return true, false
}

// copyValue returns v copied through its Copier implementation, or v itself when it has none or is a nil
// pointer. The assertion is made against the value rather than T's zero value, because a zero value answers the
// wrong question for an interface T: a nil interface implements nothing. Pass nilable from copyTraits so the
// reflection isNil needs stays off the paths that cannot hold a nil.
func copyValue[T any](v T, nilable bool) T {
	c, ok := any(v).(Copier[T])
	if !ok || (nilable && isNil(v)) {
		return v
	}
	return c.Copy()
}

// isNil reports whether v is a value Copy must not be called through: a nil interface, a nil pointer, or an
// interface holding a typed nil pointer. A nil map, slice, channel or func is deliberately not one of these — a
// value-receiver method on them is a legal call, so their own Copy decides what a nil copies to. It reflects
// through a pointer to v so that reflect sees T itself: passing an interface value as any would unwrap it to the
// concrete value inside.
func isNil[T any](v T) bool {
	rv := reflect.ValueOf(&v).Elem()
	if rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return true
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Pointer, reflect.UnsafePointer:
		return rv.IsNil()
	}
	return false
}
