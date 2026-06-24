//go:generate stringer -type=Color -marshal -reverse color.go

package main

type Color int

const (
	Red Color = iota
	Green
	Blue
	Yellow
)
