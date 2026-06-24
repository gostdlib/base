package data

import "fmt"

//go:generate immutable -type Generic

// Generic comment.
type Generic struct {
	// ID comment.
	ID   uint64
	Name string
}

// String method comment.
func (g *Generic) String() string {
	return fmt.Sprintf("%+v", g)
}

// Rename mutates a field through a receiver that is not named "r". The mutation guard must
// still detect it and refuse to generate an immutable version.
func (g *Generic) Rename() {
	g.Name = "changed"
}
