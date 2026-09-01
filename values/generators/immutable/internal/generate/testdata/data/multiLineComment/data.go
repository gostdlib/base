package data

//go:generate immutable -type MultiLine

// MultiLine exercises doc comments that span more than one line. ast.CommentGroup.Text() strips the "//" markers
// but keeps the line breaks, so every line after the first needs its own marker in the generated file.
//
// An indented line is a code block, and it has to stay indented on the generated type:
//
//	m := MultiLine{ID: 1}
//	use(m)
//
// Otherwise the example above is flattened into prose.
type MultiLine struct {
	// ID has a comment that also spans two lines, so it must be lifted to a doc comment above the field
	// rather than collapsed into one very long trailing comment, and repeated on the getter and setter.
	//
	// The code block here must survive onto the field, the getter and the setter:
	//
	//	id := m.GetID()
	//	use(id)
	ID uint64

	// Name has a single line comment, which must keep working.
	Name string

	Tags map[string]struct{}
}
