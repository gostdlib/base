// Package data holds type declarations used to test the set generator's analysis.
package data

// Color is a string-based type with constants.
type Color string

const (
	Blue Color = "blue"
	Red  Color = "yellow"
	// green is unexported to prove unexported constants land in the set.
	green Color = "green"
)

// hue is unexported to prove unexported types can be named.
type hue string

const hueA hue = "amber"

// ID is an int-based type with constants.
type ID int

const (
	One  ID = 1
	Zero ID = 0
)

// Bare is an allowed type that has no constants declared.
type Bare float64

// Sliced does not have an allowed underlying type.
type Sliced []string

// NotAType is a constant, not a type.
const NotAType = 5
