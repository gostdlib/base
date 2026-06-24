package data

import "strconv"

//go:generate immutable -type Pair

// Pair has multiple type parameters and a value-receiver method exercising the *ast.IndexListExpr receiver form.
type Pair[T any, X any] struct {
	A T
	B X
}

// Dump uses strconv, which nothing else references; if Dump is not copied onto the immutable type the
// generated file would import strconv without using it and fail to compile.
func (p Pair[T, X]) Dump() string {
	return strconv.Itoa(42)
}
