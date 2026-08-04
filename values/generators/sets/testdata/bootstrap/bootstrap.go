// Package bootstrap does not compile: it references ColorSet, which the set generator has not
// created yet. This is the normal state of a package the moment a //go:generate set directive is
// added alongside code that uses the set, so the generator must still be able to run here.
package bootstrap

type Color string

const (
	Blue Color = "blue"
	Red  Color = "red"
)

var _ = ColorSet.Contains(Blue)
