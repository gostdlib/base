package data

import (
	"fmt"
	"io"
)

//go:generate immutable -type Generic

// Record comment.
type Generic[T any] struct {
	// ID comment.
	ID    uint64
	Name  string
	Email string

	Tags          map[string]struct{}
	SlicesGeneric []T
	Slices        []int

	SubData T
	Inter   io.Reader // This one and the one below can't be the same name.
	inter   io.Writer // When the one above is made private, it becomes the same name as this

	private string
}

// String method comment.
func (r *Generic[T]) String() string {
	return fmt.Sprintf("%+v", r)
}

func (r *Generic[T]) privateMethod() string {
	return fmt.Sprintf("%+v", r)
}

func (r Generic[T]) DoNotHavePtrReceiver() string {
	return fmt.Sprintf("%+v", r)
}
