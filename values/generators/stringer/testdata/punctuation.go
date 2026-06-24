//go:generate stringer -type=Punctuation -reverse -replace=\,,_ punctuation.go

package main

type Punctuation int

const (
	Under_Score Punctuation = iota
	Hyphen_Dash
	Period_Dot
)
