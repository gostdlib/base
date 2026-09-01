package data

import "github.com/gostdlib/base/values/generators/immutable/internal/generate/testdata/data/immutable"

//go:generate immutable -type Sidecar

// Unrelated uses the foreign immutable package. It is not the generation target, so its import must not be
// mistaken for a collision: the generated file never receives imports the target does not use.
type Unrelated struct {
	M immutable.Map[string, int]
}

// Sidecar is the target. Its raw map is wrapped, so the generated file imports the real immutable package —
// legally, because the foreign one stays behind with Unrelated.
type Sidecar struct {
	Raw map[string]int
}
