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

	BenchmarkInt/Set-10                             1000000000               0.3142 ns/op          0 B/op          0 allocs/op
	BenchmarkInt/Unset-10                           1000000000               0.3140 ns/op          0 B/op          0 allocs/op
	BenchmarkInt/V-10                               1000000000               0.3156 ns/op          0 B/op          0 allocs/op
	BenchmarkInt/IsSet-10                           1000000000               0.3148 ns/op          0 B/op          0 allocs/op
	BenchmarkInt/MarshalJSON-10                     14314348                82.78 ns/op           16 B/op          2 allocs/op
	BenchmarkInt/MarshalJSONV2-10                   76317876                15.72 ns/op            0 B/op          0 allocs/op
	BenchmarkInt/UnmarshalJSON-10                   14442656                82.24 ns/op           16 B/op          2 allocs/op
	BenchmarkInt/UnmarshalJSONV2-10                  3822488               309.4 ns/op            64 B/op          1 allocs/op

Benchmark note: I expect BenchmarkInt/UnmarshalJSONV2-10 time of 309.4 ns/op to be significantly lower on a real system.
Testing this is a little funky because you have to re-create the JSON decoder each time. Even with starting and
stopping the timer in the test, the time is still higher than I would expect in the real world.
I think this is due to the test harness and not the actual performance of the code.

Note: This does not use a single generic type because the json unmarshalling in the v2 package required type detection at runtime.
By not using a generic version, we already know what broad type we are dealing with and can avoid the type detection.
This allows us to have lower allocations with JSON encoding/decoding.
*/
package isset

import (
	"unsafe"
)

// bytesToStr converts a byte slice to a string without copying the data.
func bytesToStr(b []byte) string {
	l := len(b)
	if l == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), l)
}
