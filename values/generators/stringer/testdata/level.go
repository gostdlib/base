// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test with -reverse and -invalid flags together.
//
// go:generate stringer -type=Level -reverse -invalid="0"
package main

import (
	"fmt"
	"os"
)

type Level int

const (
	Undefined Level = 0  // This should be invalid due to "0" in -invalid
	Low       Level = 1
	Medium    Level = 2
	High      Level = 3
	Critical  Level = 4
)

func main() {
	// Test String() method
	if Low.String() != "Low" {
		fmt.Fprintf(os.Stderr, "Level.String() failed: got %q, want %q\n", Low.String(), "Low")
		os.Exit(1)
	}

	// Test Reverse function - valid value
	reversedLevel, ok := ReverseLevel("Medium", true)
	if !ok || reversedLevel != Medium {
		fmt.Fprintf(os.Stderr, "ReverseLevel(\"Medium\", true) failed: got (%v, %v), want (%v, true)\n", reversedLevel, ok, Medium)
		os.Exit(1)
	}

	// Test Reverse function - invalid value (Undefined = 0)
	reversedLevel, ok = ReverseLevel("Undefined", true)
	if ok {
		fmt.Fprintf(os.Stderr, "ReverseLevel(\"Undefined\", true) should return !ok because 0 is invalid: got (%v, %v), want (_, false)\n", reversedLevel, ok)
		os.Exit(1)
	}

	// Test Reverse function - case insensitive
	reversedLevel, ok = ReverseLevel("high", false)
	if !ok || reversedLevel != High {
		fmt.Fprintf(os.Stderr, "ReverseLevel(\"high\", false) failed: got (%v, %v), want (%v, true)\n", reversedLevel, ok, High)
		os.Exit(1)
	}

	// Test Reverse function - invalid value case insensitive
	reversedLevel, ok = ReverseLevel("undefined", false)
	if ok {
		fmt.Fprintf(os.Stderr, "ReverseLevel(\"undefined\", false) should return !ok because 0 is invalid: got (%v, %v), want (_, false)\n", reversedLevel, ok)
		os.Exit(1)
	}
}
