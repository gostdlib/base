package data

import "github.com/gostdlib/base/values/immutable"

//go:generate immutable -type PreDeclared

//	pre := PreDeclared{}
//	use(pre)
//
// PreDeclared opens its doc comment with a code block, which must stay a code block, and holds a field
// the author already wrote as an immutable type. That field is passed through rather than wrapped again,
// and the immutable import must not be emitted twice.
type PreDeclared struct {
	// Mapping is already immutable, so the conversions leave it alone.
	Mapping immutable.Map[string, int]

	// Raw is a plain map, so the generator wraps it and the conversions copy.
	Raw map[string]int
}
