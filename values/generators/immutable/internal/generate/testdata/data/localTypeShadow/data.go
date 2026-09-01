package data

//go:generate immutable -type Shadowed

// helper declares a function-local type with the target's name. ast.Inspect visits it, so the generator has to
// skip declarations that are not at file scope rather than mistake this one for the target.
func helper() int {
	type Shadowed int
	return int(Shadowed(1))
}

// Shadowed is the real, file-scope target.
type Shadowed struct {
	// Name is a plain scalar field.
	Name string
}
