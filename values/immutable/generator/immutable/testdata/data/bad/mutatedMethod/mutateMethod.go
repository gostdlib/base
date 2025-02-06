package data

import "fmt"

//go:generate immutable -type Generic

// Record comment.
type Generic[T any, X comparable] struct {
	// ID comment.
	ID    uint64
	Name  string
	Email string

	Tags map[string]struct{}

	SubData T
	Comp    X
}

// String method comment.
func (r *Generic[T, X]) String() string {
	return fmt.Sprintf("%+v", r)
}

func (r *Generic[T, X]) ChangeThings() {
	r.ID = 23 // This is changing a field that would become immutable.
}
