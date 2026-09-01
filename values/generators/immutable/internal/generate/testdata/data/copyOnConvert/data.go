package data

//go:generate immutable -type Copied -copy

// Copied is generated with -copy, so Immutable() copies its map and slice and the mutable struct stays
// usable afterwards.
type Copied struct {
	// Tags is copied on conversion rather than shared.
	Tags map[string]string

	// Names is copied on conversion rather than shared.
	Names []string
}
