// Package unsafe provides functions that allow you to bypass the immutability restrictions on
// the immutable package. These functions are unsafe and should be used with caution.
//
// Note: This package is unfinished until 1.24 is released, it will then target ./internal/immutable/
package unsafe

import (
	"github.com/gostdlib/base/values/immutable"

	_ "unsafe"
)

// Map returns the underlying map. This is unsafe because it allows the caller to modify the map.
func Map[K comparable, V any](m immutable.Map[K, V]) map[K]V {
	panic("this is a placeholder until 1.24")
}

// Slice returns the underlying slice. This is unsafe because it allows the caller to modify the slice.
func Slice[T any](s immutable.Slice[T]) []T {
	panic("this is a placeholder until 1.24")
}
