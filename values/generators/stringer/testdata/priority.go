// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test with -valid and -invalid flags.
//
// go:generate stringer -type=Priority -valid -invalid="0,>10"
package main

import (
	"fmt"
	"os"
)

type Priority int

const (
	Undefined Priority = 0 // This should be invalid due to "0" in -valid
	Low       Priority = 1
	Medium    Priority = 2
	High      Priority = 3
	Critical  Priority = 11 // This should be invalid due to ">10" in -valid
	Emergency Priority = 12 // This should be invalid due to ">10" in -valid
)

func main() {
	// Test String() method
	if Low.String() != "Low" {
		fmt.Fprintf(os.Stderr, "Priority.String() failed: got %q, want %q\n", Low.String(), "Low")
		os.Exit(1)
	}

	// Test Valid() method - valid values
	if !Low.Valid() {
		fmt.Fprintf(os.Stderr, "Low.Valid() failed: got false, want true\n")
		os.Exit(1)
	}

	if !Medium.Valid() {
		fmt.Fprintf(os.Stderr, "Medium.Valid() failed: got false, want true\n")
		os.Exit(1)
	}

	if !High.Valid() {
		fmt.Fprintf(os.Stderr, "High.Valid() failed: got false, want true\n")
		os.Exit(1)
	}

	// Test Valid() method - invalid value 0
	if Undefined.Valid() {
		fmt.Fprintf(os.Stderr, "Undefined.Valid() failed: got true, want false (0 is marked invalid)\n")
		os.Exit(1)
	}

	// Test Valid() method - invalid values >10
	if Critical.Valid() {
		fmt.Fprintf(os.Stderr, "Critical.Valid() failed: got true, want false (11 > 10)\n")
		os.Exit(1)
	}

	if Emergency.Valid() {
		fmt.Fprintf(os.Stderr, "Emergency.Valid() failed: got true, want false (12 > 10)\n")
		os.Exit(1)
	}

	// Test undefined value
	undefinedPriority := Priority(100)
	if undefinedPriority.Valid() {
		fmt.Fprintf(os.Stderr, "Priority(100).Valid() failed: got true, want false\n")
		os.Exit(1)
	}
}
