// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test with -valid and -reverse flags.
//
// go:generate stringer -type=Status -valid -reverse
package main

import (
	"fmt"
	"os"
)

type Status int

const (
	Pending Status = iota
	Active
	Completed
	Failed
)

func main() {
	// Test String() method
	if Pending.String() != "Pending" {
		fmt.Fprintf(os.Stderr, "Status.String() failed: got %q, want %q\n", Pending.String(), "Pending")
		os.Exit(1)
	}

	// Test Valid() method
	if !Pending.Valid() {
		fmt.Fprintf(os.Stderr, "Pending.Valid() failed: got false, want true\n")
		os.Exit(1)
	}

	invalidStatus := Status(100)
	if invalidStatus.Valid() {
		fmt.Fprintf(os.Stderr, "Status(100).Valid() failed: got true, want false\n")
		os.Exit(1)
	}

	// Test Reverse function - case sensitive
	reversedStatus, ok := ReverseStatus("Active", true)
	if !ok || reversedStatus != Active {
		fmt.Fprintf(os.Stderr, "ReverseStatus(\"Active\", true) failed: got (%v, %v), want (%v, true)\n", reversedStatus, ok, Active)
		os.Exit(1)
	}

	// Test Reverse function - case insensitive
	reversedStatus, ok = ReverseStatus("active", false)
	if !ok || reversedStatus != Active {
		fmt.Fprintf(os.Stderr, "ReverseStatus(\"active\", false) failed: got (%v, %v), want (%v, true)\n", reversedStatus, ok, Active)
		os.Exit(1)
	}

	// Test Reverse function - not found
	reversedStatus, ok = ReverseStatus("Unknown", true)
	if ok {
		fmt.Fprintf(os.Stderr, "ReverseStatus(\"Unknown\", true) failed: got (%v, %v), want (_, false)\n", reversedStatus, ok)
		os.Exit(1)
	}
}
