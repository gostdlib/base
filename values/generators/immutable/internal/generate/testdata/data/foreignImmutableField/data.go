package data

import "github.com/gostdlib/base/values/generators/immutable/internal/generate/testdata/data/immutable"

//go:generate immutable -type Passthrough

// Passthrough's only field is a foreign type that happens to be called immutable.Map. It is not this generator's
// Map, so the field is passed through untouched, no immutable import of our own is emitted, and there is nothing
// for the foreign import to collide with.
type Passthrough struct {
	M immutable.Map[string, int]
}
