package reset

import (
	"testing"
)

type Works struct {
	A int
	b string // private field
	C bool
	M map[string]string
	m map[string]string
	S []int
	s []int

	Ignore    int
	ignore    int
	Default10 int
	default10 int
	Ptr       *int
	ptr       *int
}

func (e *Works) Reset() {
	e.A = 0
	e.b = ""
	e.C = false
	e.M = nil
	e.m = nil
	e.S = nil
	e.s = nil
	e.Default10 = 10
	e.default10 = 10
	e.Ptr = nil
	e.ptr = nil
}

func TestReset(t *testing.T) {
	fields := Fields{
		Ignore:   []string{"Ignore", "ignore"},
		HasValue: map[string]any{"Default10": 10, "default10": 10},
	}

	err := Validate[*Works](fields)
	if err != nil {
		t.Errorf("TestReset: %s", err)
	}
}
