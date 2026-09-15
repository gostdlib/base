/*
Package isset provides a way to provide basic values that can be checked if they were set
instead of using pointers to basic types which are costly on the garbage collector.

Aka, going from this:

	var v *int

	... // More code

	if v != nil {
		// Do something with v.
	}

To this:

	var v isset.Int

	... // More code

	if v.IsSet() {
		// Do something with v.
	}
	fmt.Println(v.V())

This is useful when the zero value is a valid value and you need to check if the value was set or not. This avoids
nil checks on pointers to basic types and nil values that can cause panics.

This type of thing is common with configuration files where you want to know if a value was set or not. This
package supports JSON marshalling and unmarshalling using the v1 an v2 JSON packages.

Note: The types in this package do not use pointers, but return values. This is to avoid heap allocations
and to keep the values on the stack.

Example:

	type MyStruct struct {
		Val isset.Int
	}

	// Create a new MyStruct.
	ms := MyStruct{}

	// Check if the value was set.
	// This will return false.
	fmt.Println(ms.Val.IsSet())

	// Set the zero value.
	ms.Val = ms.Val.Set(0) // You must do a reassignment.
	// This will return true.
	fmt.Println(ms.Val.IsSet())

	// Get the value.
	// This will return 0.
	fmt.Println(ms.Val.V())

Benchmarks:

	BenchmarkInt/Set-10                             1000000000               0.3239 ns/op          0 B/op          0 allocs/op
	BenchmarkInt/Unset-10                           1000000000               0.3337 ns/op          0 B/op          0 allocs/op
	BenchmarkInt/V-10                               1000000000               0.3183 ns/op          0 B/op          0 allocs/op
	BenchmarkInt/IsSet-10                           1000000000               0.3166 ns/op          0 B/op          0 allocs/op
	BenchmarkInt/MarshalJSON-10                     14258449                82.80 ns/op           16 B/op          2 allocs/op
	BenchmarkInt/MarshalJSONTo-10                   73467886                16.44 ns/op            0 B/op          0 allocs/op
	BenchmarkInt/UnmarshalJSON-10                   13472341                93.45 ns/op           16 B/op          2 allocs/op
	BenchmarkInt/UnmarshalJSONFrom-10               17886554                66.00 ns/op            0 B/op          0 allocs/op

UnmarshalJSONFrom includes resetting the decoder on every iteration.

Note: This does not use a single generic type because the json unmarshalling in the v2 package required type detection at runtime.
By not using a generic version, we already know what broad type we are dealing with and can avoid the type detection.
This allows us to have lower allocations with JSON encoding/decoding.
*/
package isset

import (
	"unsafe"

	"github.com/go-json-experiment/json"
)

// Compile time assertions that every type here implements the json v2 marshaling interfaces. Without these it is
// easy to rename a method and silently fall back to the slower v1 reflection path.
var (
	_ json.MarshalerTo     = Bool{}
	_ json.UnmarshalerFrom = (*Bool)(nil)
	_ json.MarshalerTo     = String{}
	_ json.UnmarshalerFrom = (*String)(nil)
	_ json.MarshalerTo     = Int{}
	_ json.UnmarshalerFrom = (*Int)(nil)
	_ json.MarshalerTo     = Uint{}
	_ json.UnmarshalerFrom = (*Uint)(nil)
	_ json.MarshalerTo     = Float64{}
	_ json.UnmarshalerFrom = (*Float64)(nil)
)

// bytesToStr converts a byte slice to a string without copying the data.
func bytesToStr(b []byte) string {
	l := len(b)
	if l == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), l)
}
