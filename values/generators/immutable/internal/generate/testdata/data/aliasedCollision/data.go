package data

import immutable "example.com/other/thing"

//go:generate immutable -type Aliased

// Aliased refers to a foreign package under the alias immutable and also carries a raw map, so the generated
// file would need this generator's immutable package under the same name. The alias, not the path, is what the
// file calls the package, so the collision has to be caught through the alias.
type Aliased struct {
	Mapping immutable.Map[string, int]
	Raw     map[string]int
}
