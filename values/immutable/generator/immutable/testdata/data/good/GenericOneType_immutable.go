
// Code generated by immutable tool. DO NOT EDIT.

package data

import (
	"github.com/gostdlib/base/values/immutable"
	
	"fmt"
	"io"
)

// ImGenericOneType[T any] is an immutable version of GenericOneType[T any].
// Record comment.
type ImGenericOneType[T any] struct {
	id uint64 // ID comment.
	name string 
	email string 
	tags immutable.Map[string, struct{}] 
	slicesGeneric immutable.Slice[T] 
	slices immutable.Slice[int] 
	subData T 
	inter io.Reader 
	private string 
}

// GetID retrieves the content of the field ID.
// ID comment.
func (r *ImGenericOneType[T]) GetID() uint64 {
	return r.id
}

// SetID returns a copy of the struct with the field ID set to the new value.
// ID comment.
func (r *ImGenericOneType[T]) SetID(value uint64) ImGenericOneType[T] {
	n := copyImGenericOneType[T](*r)
	n.id = value
	return n
}
// GetName retrieves the content of the field Name.
func (r *ImGenericOneType[T]) GetName() string {
	return r.name
}

// SetName returns a copy of the struct with the field Name set to the new value.
func (r *ImGenericOneType[T]) SetName(value string) ImGenericOneType[T] {
	n := copyImGenericOneType[T](*r)
	n.name = value
	return n
}
// GetEmail retrieves the content of the field Email.
func (r *ImGenericOneType[T]) GetEmail() string {
	return r.email
}

// SetEmail returns a copy of the struct with the field Email set to the new value.
func (r *ImGenericOneType[T]) SetEmail(value string) ImGenericOneType[T] {
	n := copyImGenericOneType[T](*r)
	n.email = value
	return n
}
// GetTags retrieves the content of the field Tags.
func (r *ImGenericOneType[T]) GetTags() immutable.Map[string, struct{}] {
	return r.tags
}

// SetTags returns a copy of the struct with the field Tags set to the new value.
func (r *ImGenericOneType[T]) SetTags(value immutable.Map[string, struct{}]) ImGenericOneType[T] {
	n := copyImGenericOneType[T](*r)
	n.tags = value
	return n
}
// GetSlicesGeneric retrieves the content of the field SlicesGeneric.
func (r *ImGenericOneType[T]) GetSlicesGeneric() immutable.Slice[T] {
	return r.slicesGeneric
}

// SetSlicesGeneric returns a copy of the struct with the field SlicesGeneric set to the new value.
func (r *ImGenericOneType[T]) SetSlicesGeneric(value immutable.Slice[T]) ImGenericOneType[T] {
	n := copyImGenericOneType[T](*r)
	n.slicesGeneric = value
	return n
}
// GetSlices retrieves the content of the field Slices.
func (r *ImGenericOneType[T]) GetSlices() immutable.Slice[int] {
	return r.slices
}

// SetSlices returns a copy of the struct with the field Slices set to the new value.
func (r *ImGenericOneType[T]) SetSlices(value immutable.Slice[int]) ImGenericOneType[T] {
	n := copyImGenericOneType[T](*r)
	n.slices = value
	return n
}
// GetSubData retrieves the content of the field SubData.
func (r *ImGenericOneType[T]) GetSubData() T {
	return r.subData
}

// SetSubData returns a copy of the struct with the field SubData set to the new value.
func (r *ImGenericOneType[T]) SetSubData(value T) ImGenericOneType[T] {
	n := copyImGenericOneType[T](*r)
	n.subData = value
	return n
}
// GetInter retrieves the content of the field Inter.
func (r *ImGenericOneType[T]) GetInter() io.Reader {
	return r.inter
}

// SetInter returns a copy of the struct with the field Inter set to the new value.
func (r *ImGenericOneType[T]) SetInter(value io.Reader) ImGenericOneType[T] {
	n := copyImGenericOneType[T](*r)
	n.inter = value
	return n
}

// Mutable converts the immutable struct back to the original mutable struct.
func (r *ImGenericOneType[T]) Mutable() GenericOneType[T] {
	return GenericOneType[T]{
		ID: r.id,
		Name: r.name,
		Email: r.email,
		Tags: r.tags.Copy(),
		SlicesGeneric: r.slicesGeneric.Copy(),
		Slices: r.slices.Copy(),
		SubData: r.subData,
		Inter: r.inter,
		private: r.private,
	}
}

// Immutable converts the mutable struct to the generated immutable struct.
func (r *GenericOneType[T]) Immutable() ImGenericOneType[T] {
	return ImGenericOneType[T]{
		id: (r.ID),
		name: (r.Name),
		email: (r.Email),
		tags: immutable.NewMap[string, struct{}](r.Tags),
		slicesGeneric: immutable.NewSlice[T](r.SlicesGeneric),
		slices: immutable.NewSlice[int](r.Slices),
		subData: (r.SubData),
		inter: (r.Inter),
		private: (r.private),
	}
}

func copyImGenericOneType[T any](s ImGenericOneType[T]) ImGenericOneType[T] {
	return s
}

// String is a copy of the original method from GenericOneType.
func (r *ImGenericOneType[T]) String() string {
    return fmt.Sprintf("%+v", "GenericOneType")
}

// privateMethod is a copy of the original method from GenericOneType.
func (r *ImGenericOneType[T]) privateMethod() string {
    return fmt.Sprintf("%+v", "private method")
}

// DoNotHavePtrReceiver is a copy of the original method from GenericOneType.
func (r ImGenericOneType[T]) DoNotHavePtrReceiver() string {
    return fmt.Sprintf("%+v", "okay")
}
