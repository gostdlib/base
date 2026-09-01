package data_test

// Shadowed here is an external test package's own type sharing the target's name. It sorts before the real
// declaration, so it pins SkipFile: parse this file and generation fails on a name that is not a struct.
type Shadowed int
