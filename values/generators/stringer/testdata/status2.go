//go:generate stringer -type=Status2 -reverse -replace=-,_ status2.go

package main

type Status2 int

const (
	In_Progress Status2 = iota
	Not_Started
	Completed
	Failed
)
