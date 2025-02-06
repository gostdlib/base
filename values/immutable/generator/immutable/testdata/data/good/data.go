package data

import (
	"fmt"
	"io"
)

//go:generate immutable -type Generic

// Record comment.
type Generic[T any, X comparable] struct {
	// ID comment.
	ID    uint64
	Name  string
	Email string

	Tags          map[string]struct{}
	SlicesGeneric []T
	Slices        []int

	SubData T
	Comp    X

	private string
}

// String method comment.
func (r *Generic[T, X]) String() string {
	return fmt.Sprintf("%+v", "Generic")
}

func (r *Generic[T, X]) privateMethod() string {
	return fmt.Sprintf("%+v", "private method")
}

func (r Generic[T, X]) DoNotHavePtrReceiver() string {
	return fmt.Sprintf("%+v", "okay")
}

//go:generate immutable -type GenericOneType

// Record comment.
type GenericOneType[T any] struct {
	// ID comment.
	ID    uint64
	Name  string
	Email string

	Tags          map[string]struct{}
	SlicesGeneric []T
	Slices        []int

	SubData T
	Inter   io.Reader

	private string
}

// String method comment.
func (r *GenericOneType[T]) String() string {
	return fmt.Sprintf("%+v", "GenericOneType")
}

func (r *GenericOneType[T]) privateMethod() string {
	return fmt.Sprintf("%+v", "private method")
}

func (r GenericOneType[T]) DoNotHavePtrReceiver() string {
	return fmt.Sprintf("%+v", "okay")
}

//go:generate immutable -type NonGeneric

type NonGeneric struct {
	ID     uint64
	Name   string
	Tags   map[string]struct{}
	Slices []int

	private    string
	privatePtr *string
	PublicPtr  *string
	inter      io.Writer
}

// String method comment.
func (r *NonGeneric) String() string {
	return fmt.Sprintf("%+v", "NonGeneric")
}

func (r *NonGeneric) privateMethod() string {
	return fmt.Sprintf("%+v", "private method")
}

func (r NonGeneric) DoNotHavePtrReceiver() string {
	return fmt.Sprintf("%+v", "okay")
}
