// Package immutable is a stand-in for a foreign package that happens to share the name of this generator's
// immutable package. The testdata packages importing it prove the collision check fires only when this package
// would actually be carried into a generated file.
package immutable

// Map is a minimal stand-in for a foreign map type.
type Map[K comparable, V any] struct {
	m map[K]V
}
