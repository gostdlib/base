package data

import "example.com/other/immutable"

// Collides declares a field from a different package that is also called immutable, alongside a raw slice the
// generator wraps. The generated file needs its own immutable package under that name, so the two cannot coexist
// and generation has to say so rather than emit both imports.
type Collides struct {
	Mapping immutable.Map[string, int]
	Raw     []int
}
