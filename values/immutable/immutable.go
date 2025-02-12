/*
Package immutable is a utility package that is used by the immutable generator and holds types that provide
immutability for standard Go types. The generatator, called "immutable" is similar to
the stringer tool. It extends the type provided with the "-type" flag have an "Immutable()" method to generate
an immutable version. That version moves all the fields to private and provides getters. If the type is a
map or slice, it wraps those in the ones provided here. It also provide's setters for each field, but those
return a new immutable value that does not change the original, keeping immutability intact. Finally it generates
a "Mutable()" method to create a copy of the immutable version that can be changed.

It supports non-generic and generic types. It does not currently support field tags.

For example, given this:

	package blah

	//go:generate immutable -type MyType

	// MyType is a type that contains things.
	type MyType struct {
		// Name is the name of something.
		Name string
		Mapping map[string]string
		Slicing []int
		Ptr *string
		private int
	}

The package would get a file: MyType_immutable.go with contents:

	package blah

	import (
		"github.com/gostdlib/base/values/immutable"
	)

	// ImMyType is an immutable version of MyType.
	// MyType is a type that contains things.
	type ImMyType struct {
		name string // Name is the name of something.
		mapping immutable.Map[string, string] //
		slicing immutable.Slice[int] //
		ptr *string //
		private int //
	}

	// GetName retrieves the content of the field Name.
	// Name is the name of something.
	func (r *ImMyType) GetName() string {
		return r.name
	}

	// SetName returns a copy of the struct with the field Name set to the new value.
	// Name is the name of something.
	func (r *ImMyType) SetName(value string) ImMyType {
		n := copyImMyType(*r)
		n.name = value
		return n
	}
	// GetMapping retrieves the content of the field Mapping.
	func (r *ImMyType) GetMapping() immutable.Map[string, string] {
		return r.mapping
	}

	// SetMapping returns a copy of the struct with the field Mapping set to the new value.
	func (r *ImMyType) SetMapping(value immutable.Map[string, string]) ImMyType {
		n := copyImMyType(*r)
		n.mapping = value
		return n
	}
	// GetSlicing retrieves the content of the field Slicing.
	func (r *ImMyType) GetSlicing() immutable.Slice[int] {
		return r.slicing
	}

	// SetSlicing returns a copy of the struct with the field Slicing set to the new value.
	func (r *ImMyType) SetSlicing(value immutable.Slice[int]) ImMyType {
		n := copyImMyType(*r)
		n.slicing = value
		return n
	}
	// GetPtr retrieves the content of the field Ptr.
	func (r *ImMyType) GetPtr() *string {
		return r.ptr
	}

	// SetPtr returns a copy of the struct with the field Ptr set to the new value.
	func (r *ImMyType) SetPtr(value *string) ImMyType {
		n := copyImMyType(*r)
		n.ptr = value
		return n
	}

	// Mutable converts the immutable struct back to the original mutable struct.
	func (r *ImMyType) Mutable() MyType {
		return MyType{
			Name: r.name,
			Mapping: r.mapping.Copy(),
			Slicing: r.slicing.Copy(),
			Ptr: r.ptr,
			private: r.private,
		}
	}

	// Immutable converts the mutable struct to the generated immutable struct.
	func (r *MyType) Immutable() ImMyType {
		return ImMyType{
			name: (r.Name),
			mapping: immutable.NewMap[string, string](r.Mapping),
			slicing: immutable.NewSlice[int](r.Slicing),
			ptr: (r.Ptr),
			private: (r.private),
		}
	}

	func copyImMyType(s ImMyType) ImMyType {
		return s
	}
*/
package immutable

import (
	"github.com/gostdlib/base/values/immutable/internal/immutable"
)

// Map provides a read-only map as long as the values are not pointers or references.
type Map[K comparable, V any] = immutable.Map[K, V]

// NewMap returns a new immutable map.
func NewMap[K comparable, V any](m map[K]V) Map[K, V] {
	return immutable.NewMap(m)
}

// Slice provides a read-only slice as long as the values are not pointers or references.
type Slice[T any] = immutable.Slice[T]

// NewSlice returns a new immutable slice.
func NewSlice[T any](s []T) Slice[T] {
	return immutable.NewSlice(s)
}

// Copier is an interface that allows a type to be copied. This is useful when the value stored
// in the immutable type is a pointer or reference. This allows a deep copy to be made if the
// type implements this interface.
type Copier[T any] = immutable.Copier[T]

// CopySlice returns a copy of the given slice. This is useful for creating an immutable slice.
// If the type stored in the slice implements the Copier interface, it will use that to copy the
// values. Otherwise, it will use the standard copy function.
func CopySlice[T any](s []T) []T {
	return immutable.CopySlice(s)
}

// CopyMap returns a copy of the given map. This is useful for creating an immutable map.
// If the type stored in the map implements the Copier interface, it will use that to copy the
// values. Otherwise, it will use the standard copy function.
func CopyMap[K comparable, V any](m map[K]V) map[K]V {
	return immutable.CopyMap(m)
}
