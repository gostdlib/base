package data

import "fmt"

//go:generate immutable -type Unnamed

// Unnamed exercises a method with an unnamed receiver, which the generator must skip without panicking.
type Unnamed struct {
	ID   uint64
	Name string
}

// Greet has an unnamed receiver so it cannot reference any field; the generator skips it. Its use of the
// fmt package must not leak into the generated file's imports (it would be an unused import otherwise).
func (*Unnamed) Greet() string {
	return fmt.Sprintf("%+v", "hello")
}
