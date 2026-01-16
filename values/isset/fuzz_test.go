package isset

import (
	"bytes"
	"testing"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// FuzzIntUnmarshalJSON tests that Int.UnmarshalJSON handles arbitrary input without panicking.
func FuzzIntUnmarshalJSON(f *testing.F) {
	f.Add([]byte("42"))
	f.Add([]byte("-42"))
	f.Add([]byte("0"))
	f.Add([]byte("null"))
	f.Add([]byte(""))
	f.Add([]byte("9223372036854775807"))  // max int64
	f.Add([]byte("-9223372036854775808")) // min int64
	f.Add([]byte("1.5"))
	f.Add([]byte(`"string"`))
	f.Add([]byte("true"))
	f.Add([]byte("false"))
	f.Add([]byte("[]"))
	f.Add([]byte("{}"))
	f.Add([]byte("invalid"))
	f.Add([]byte("\x00\x00"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v Int
		_ = v.UnmarshalJSON(data)
	})
}

// FuzzIntUnmarshalJSONV2 tests that Int.UnmarshalJSONV2 handles arbitrary input without panicking.
func FuzzIntUnmarshalJSONV2(f *testing.F) {
	f.Add([]byte("42"))
	f.Add([]byte("-42"))
	f.Add([]byte("0"))
	f.Add([]byte("null"))
	f.Add([]byte("9223372036854775807"))
	f.Add([]byte("-9223372036854775808"))
	f.Add([]byte("1.5"))
	f.Add([]byte(`"string"`))
	f.Add([]byte("true"))
	f.Add([]byte("[]"))
	f.Add([]byte("{}"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v Int
		dec := jsontext.NewDecoder(bytes.NewReader(data))
		_ = v.UnmarshalJSONV2(dec, json.DefaultOptionsV2())
	})
}

// FuzzInt64UnmarshalJSON tests that Int64.UnmarshalJSON handles arbitrary input without panicking.
func FuzzInt64UnmarshalJSON(f *testing.F) {
	f.Add([]byte("42"))
	f.Add([]byte("-42"))
	f.Add([]byte("0"))
	f.Add([]byte("null"))
	f.Add([]byte("9223372036854775807"))
	f.Add([]byte("-9223372036854775808"))
	f.Add([]byte("18446744073709551615")) // overflow
	f.Add([]byte("1e100"))                // scientific notation

	f.Fuzz(func(t *testing.T, data []byte) {
		var v Int64
		_ = v.UnmarshalJSON(data)
	})
}

// FuzzUintUnmarshalJSON tests that Uint.UnmarshalJSON handles arbitrary input without panicking.
func FuzzUintUnmarshalJSON(f *testing.F) {
	f.Add([]byte("42"))
	f.Add([]byte("0"))
	f.Add([]byte("null"))
	f.Add([]byte("18446744073709551615")) // max uint64
	f.Add([]byte("-1"))                   // negative
	f.Add([]byte("1.5"))
	f.Add([]byte(`"string"`))
	f.Add([]byte("true"))
	f.Add([]byte("[]"))
	f.Add([]byte("{}"))
	f.Add([]byte("invalid"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v Uint
		_ = v.UnmarshalJSON(data)
	})
}

// FuzzUintUnmarshalJSONV2 tests that Uint.UnmarshalJSONV2 handles arbitrary input without panicking.
func FuzzUintUnmarshalJSONV2(f *testing.F) {
	f.Add([]byte("42"))
	f.Add([]byte("0"))
	f.Add([]byte("null"))
	f.Add([]byte("18446744073709551615"))
	f.Add([]byte("-1"))
	f.Add([]byte("1.5"))
	f.Add([]byte(`"string"`))
	f.Add([]byte("true"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v Uint
		dec := jsontext.NewDecoder(bytes.NewReader(data))
		_ = v.UnmarshalJSONV2(dec, json.DefaultOptionsV2())
	})
}

// FuzzUint64UnmarshalJSON tests that Uint64.UnmarshalJSON handles arbitrary input without panicking.
func FuzzUint64UnmarshalJSON(f *testing.F) {
	f.Add([]byte("42"))
	f.Add([]byte("0"))
	f.Add([]byte("null"))
	f.Add([]byte("18446744073709551615"))
	f.Add([]byte("18446744073709551616")) // overflow
	f.Add([]byte("-1"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v Uint64
		_ = v.UnmarshalJSON(data)
	})
}

// FuzzFloat64UnmarshalJSON tests that Float64.UnmarshalJSON handles arbitrary input without panicking.
func FuzzFloat64UnmarshalJSON(f *testing.F) {
	f.Add([]byte("42.5"))
	f.Add([]byte("-42.5"))
	f.Add([]byte("0"))
	f.Add([]byte("0.0"))
	f.Add([]byte("null"))
	f.Add([]byte("1.7976931348623157e+308")) // max float64
	f.Add([]byte("-1.7976931348623157e+308"))
	f.Add([]byte("5e-324")) // min positive float64
	f.Add([]byte("1e309"))  // overflow
	f.Add([]byte("NaN"))
	f.Add([]byte("Infinity"))
	f.Add([]byte("-Infinity"))
	f.Add([]byte(`"string"`))
	f.Add([]byte("true"))
	f.Add([]byte("[]"))
	f.Add([]byte("{}"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v Float64
		_ = v.UnmarshalJSON(data)
	})
}

// FuzzFloat64UnmarshalJSONV2 tests that Float64.UnmarshalJSONV2 handles arbitrary input without panicking.
func FuzzFloat64UnmarshalJSONV2(f *testing.F) {
	f.Add([]byte("42.5"))
	f.Add([]byte("-42.5"))
	f.Add([]byte("0"))
	f.Add([]byte("null"))
	f.Add([]byte("1.7976931348623157e+308"))
	f.Add([]byte("1e309"))
	f.Add([]byte("NaN"))
	f.Add([]byte("Infinity"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v Float64
		dec := jsontext.NewDecoder(bytes.NewReader(data))
		_ = v.UnmarshalJSONV2(dec, json.DefaultOptionsV2())
	})
}

// FuzzFloat32UnmarshalJSON tests that Float32.UnmarshalJSON handles arbitrary input without panicking.
func FuzzFloat32UnmarshalJSON(f *testing.F) {
	f.Add([]byte("42.5"))
	f.Add([]byte("-42.5"))
	f.Add([]byte("0"))
	f.Add([]byte("null"))
	f.Add([]byte("3.4028235e+38")) // max float32
	f.Add([]byte("1e39"))          // overflow for float32

	f.Fuzz(func(t *testing.T, data []byte) {
		var v Float32
		_ = v.UnmarshalJSON(data)
	})
}

// FuzzStringUnmarshalJSON tests that String.UnmarshalJSON handles arbitrary input without panicking.
func FuzzStringUnmarshalJSON(f *testing.F) {
	f.Add([]byte(`"hello"`))
	f.Add([]byte(`""`))
	f.Add([]byte("null"))
	f.Add([]byte(`"hello\nworld"`))
	f.Add([]byte(`"hello\u0000world"`)) // null byte in string
	f.Add([]byte(`"\u0048\u0065\u006c\u006c\u006f"`))
	f.Add([]byte(`"emoji: \ud83d\ude00"`)) // emoji
	f.Add([]byte(`"unterminated`))
	f.Add([]byte("42"))
	f.Add([]byte("true"))
	f.Add([]byte("[]"))
	f.Add([]byte("{}"))
	f.Add([]byte(`"\"`))  // escape at end
	f.Add([]byte(`"\\"`)) // escaped backslash
	f.Add([]byte(`"a very long string that might cause issues if there are buffer problems in the implementation"`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v String
		_ = v.UnmarshalJSON(data)
	})
}

// FuzzStringUnmarshalJSONV2 tests that String.UnmarshalJSONV2 handles arbitrary input without panicking.
func FuzzStringUnmarshalJSONV2(f *testing.F) {
	f.Add([]byte(`"hello"`))
	f.Add([]byte(`""`))
	f.Add([]byte("null"))
	f.Add([]byte(`"hello\nworld"`))
	f.Add([]byte(`"hello\u0000world"`))
	f.Add([]byte("42"))
	f.Add([]byte("true"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v String
		dec := jsontext.NewDecoder(bytes.NewReader(data))
		_ = v.UnmarshalJSONV2(dec, json.DefaultOptionsV2())
	})
}

// FuzzBoolUnmarshalJSON tests that Bool.UnmarshalJSON handles arbitrary input without panicking.
func FuzzBoolUnmarshalJSON(f *testing.F) {
	f.Add([]byte("true"))
	f.Add([]byte("false"))
	f.Add([]byte("null"))
	f.Add([]byte("True"))
	f.Add([]byte("FALSE"))
	f.Add([]byte("1"))
	f.Add([]byte("0"))
	f.Add([]byte(`"true"`))
	f.Add([]byte(`"false"`))
	f.Add([]byte("[]"))
	f.Add([]byte("{}"))
	f.Add([]byte("tru"))
	f.Add([]byte("fals"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v Bool
		_ = v.UnmarshalJSON(data)
	})
}

// FuzzBoolUnmarshalJSONV2 tests that Bool.UnmarshalJSONV2 handles arbitrary input without panicking.
func FuzzBoolUnmarshalJSONV2(f *testing.F) {
	f.Add([]byte("true"))
	f.Add([]byte("false"))
	f.Add([]byte("null"))
	f.Add([]byte("1"))
	f.Add([]byte("0"))
	f.Add([]byte(`"true"`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var v Bool
		dec := jsontext.NewDecoder(bytes.NewReader(data))
		_ = v.UnmarshalJSONV2(dec, json.DefaultOptionsV2())
	})
}

// FuzzIntRoundTrip tests that marshaling then unmarshaling an Int preserves the value.
func FuzzIntRoundTrip(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(42))
	f.Add(int64(-42))
	f.Add(int64(9223372036854775807))
	f.Add(int64(-9223372036854775808))

	f.Fuzz(func(t *testing.T, n int64) {
		original := Int{}.Set(int(n))

		data, err := original.MarshalJSON()
		if err != nil {
			t.Fatalf("FuzzIntRoundTrip: MarshalJSON failed: %v", err)
		}

		var unmarshaled Int
		err = unmarshaled.UnmarshalJSON(data)
		if err != nil {
			t.Fatalf("FuzzIntRoundTrip: UnmarshalJSON failed: %v", err)
		}

		if unmarshaled.V() != original.V() {
			t.Errorf("FuzzIntRoundTrip: got %d, want %d", unmarshaled.V(), original.V())
		}
		if !unmarshaled.IsSet() {
			t.Errorf("FuzzIntRoundTrip: IsSet() = false, want true")
		}
	})
}

// FuzzUintRoundTrip tests that marshaling then unmarshaling a Uint preserves the value.
func FuzzUintRoundTrip(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(42))
	f.Add(uint64(18446744073709551615))

	f.Fuzz(func(t *testing.T, n uint64) {
		original := Uint{}.Set(uint(n))

		data, err := original.MarshalJSON()
		if err != nil {
			t.Fatalf("FuzzUintRoundTrip: MarshalJSON failed: %v", err)
		}

		var unmarshaled Uint
		err = unmarshaled.UnmarshalJSON(data)
		if err != nil {
			t.Fatalf("FuzzUintRoundTrip: UnmarshalJSON failed: %v", err)
		}

		if unmarshaled.V() != original.V() {
			t.Errorf("FuzzUintRoundTrip: got %d, want %d", unmarshaled.V(), original.V())
		}
		if !unmarshaled.IsSet() {
			t.Errorf("FuzzUintRoundTrip: IsSet() = false, want true")
		}
	})
}

// FuzzFloat64RoundTrip tests that marshaling then unmarshaling a Float64 preserves the value.
func FuzzFloat64RoundTrip(f *testing.F) {
	f.Add(float64(0))
	f.Add(float64(42.5))
	f.Add(float64(-42.5))
	f.Add(float64(1.7976931348623157e+308))
	f.Add(float64(5e-324))

	f.Fuzz(func(t *testing.T, n float64) {
		original := Float64{}.Set(n)

		data, err := original.MarshalJSON()
		if err != nil {
			// NaN and Inf cannot be marshaled - this is expected
			return
		}

		var unmarshaled Float64
		err = unmarshaled.UnmarshalJSON(data)
		if err != nil {
			t.Fatalf("FuzzFloat64RoundTrip: UnmarshalJSON failed: %v", err)
		}

		if unmarshaled.V() != original.V() {
			t.Errorf("FuzzFloat64RoundTrip: got %v, want %v", unmarshaled.V(), original.V())
		}
		if !unmarshaled.IsSet() {
			t.Errorf("FuzzFloat64RoundTrip: IsSet() = false, want true")
		}
	})
}

// FuzzStringRoundTrip tests that marshaling then unmarshaling a String preserves the value.
func FuzzStringRoundTrip(f *testing.F) {
	f.Add("")
	f.Add("hello")
	f.Add("hello\nworld")
	f.Add("hello\x00world")
	f.Add("emoji: 😀")
	f.Add(`quote: "test"`)
	f.Add("backslash: \\")

	f.Fuzz(func(t *testing.T, s string) {
		original := String{}.Set(s)

		data, err := original.MarshalJSON()
		if err != nil {
			t.Fatalf("FuzzStringRoundTrip: MarshalJSON failed: %v", err)
		}

		var unmarshaled String
		err = unmarshaled.UnmarshalJSON(data)
		if err != nil {
			t.Fatalf("FuzzStringRoundTrip: UnmarshalJSON failed: %v", err)
		}

		if unmarshaled.V() != original.V() {
			t.Errorf("FuzzStringRoundTrip: got %q, want %q", unmarshaled.V(), original.V())
		}
		if !unmarshaled.IsSet() {
			t.Errorf("FuzzStringRoundTrip: IsSet() = false, want true")
		}
	})
}

// FuzzBoolRoundTrip tests that marshaling then unmarshaling a Bool preserves the value.
func FuzzBoolRoundTrip(f *testing.F) {
	f.Add(true)
	f.Add(false)

	f.Fuzz(func(t *testing.T, b bool) {
		original := Bool{}.Set(b)

		data, err := original.MarshalJSON()
		if err != nil {
			t.Fatalf("FuzzBoolRoundTrip: MarshalJSON failed: %v", err)
		}

		var unmarshaled Bool
		err = unmarshaled.UnmarshalJSON(data)
		if err != nil {
			t.Fatalf("FuzzBoolRoundTrip: UnmarshalJSON failed: %v", err)
		}

		if unmarshaled.V() != original.V() {
			t.Errorf("FuzzBoolRoundTrip: got %v, want %v", unmarshaled.V(), original.V())
		}
		if !unmarshaled.IsSet() {
			t.Errorf("FuzzBoolRoundTrip: IsSet() = false, want true")
		}
	})
}
