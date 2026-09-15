//go:build goexperiment.jsonv2

// These tests prove the isset types work with the standard library's encoding/json/v2. It is on by default in Go 1.27.
// In Go 1.26 it only exists when built with GOEXPERIMENT=jsonv2, so run them with:
//
//	GOEXPERIMENT=jsonv2 go test ./values/isset/
//
// The standard library API is reached through stdlib_jsonv2_go126_test.go or stdlib_jsonv2_go127_test.go.

package isset

import (
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

// Compile time assertions that every type implements the standard library json v2 interfaces. If these fail, the
// standard library silently ignores our methods and tries to marshal the unexported struct fields instead.
var (
	_ stdlibMarshalerTo     = Bool{}
	_ stdlibUnmarshalerFrom = (*Bool)(nil)
	_ stdlibMarshalerTo     = String{}
	_ stdlibUnmarshalerFrom = (*String)(nil)
	_ stdlibMarshalerTo     = Int{}
	_ stdlibUnmarshalerFrom = (*Int)(nil)
	_ stdlibMarshalerTo     = Int8{}
	_ stdlibUnmarshalerFrom = (*Int8)(nil)
	_ stdlibMarshalerTo     = Int16{}
	_ stdlibUnmarshalerFrom = (*Int16)(nil)
	_ stdlibMarshalerTo     = Int32{}
	_ stdlibUnmarshalerFrom = (*Int32)(nil)
	_ stdlibMarshalerTo     = Int64{}
	_ stdlibUnmarshalerFrom = (*Int64)(nil)
	_ stdlibMarshalerTo     = Uint{}
	_ stdlibUnmarshalerFrom = (*Uint)(nil)
	_ stdlibMarshalerTo     = Uint8{}
	_ stdlibUnmarshalerFrom = (*Uint8)(nil)
	_ stdlibMarshalerTo     = Uint16{}
	_ stdlibUnmarshalerFrom = (*Uint16)(nil)
	_ stdlibMarshalerTo     = Uint32{}
	_ stdlibUnmarshalerFrom = (*Uint32)(nil)
	_ stdlibMarshalerTo     = Uint64{}
	_ stdlibUnmarshalerFrom = (*Uint64)(nil)
	_ stdlibMarshalerTo     = Float32{}
	_ stdlibUnmarshalerFrom = (*Float32)(nil)
	_ stdlibMarshalerTo     = Float64{}
	_ stdlibUnmarshalerFrom = (*Float64)(nil)
)

// stdlibConfig is a config struct where every field must distinguish "not provided" from the zero value.
type stdlibConfig struct {
	Name    String   `json:"name"`
	Enabled Bool     `json:"enabled"`
	Retries Int      `json:"retries"`
	Offset  Int64    `json:"offset"`
	Port    Uint16   `json:"port"`
	Ratio   Float64  `json:"ratio"`
	Ports   []Uint16 `json:"ports"`
}

// stdlibOmitConfig is stdlibConfig's shape using omitzero, which should drop unset values but keep values set to zero.
type stdlibOmitConfig struct {
	Name    String  `json:"name,omitzero"`
	Enabled Bool    `json:"enabled,omitzero"`
	Retries Int     `json:"retries,omitzero"`
	Ratio   Float64 `json:"ratio,omitzero"`
}

func TestStdlibMarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  any
		want string
	}{
		{name: "Success: a set Bool marshals to its boolean", val: Bool{}.Set(true), want: "true"},
		{name: "Success: a Bool set to false marshals to false", val: Bool{}.Set(false), want: "false"},
		{name: "Success: an unset Bool marshals to null", val: Bool{}, want: "null"},
		{name: "Success: a set String marshals to its string", val: String{}.Set("hello"), want: `"hello"`},
		{name: "Success: a String set to empty marshals to an empty string", val: String{}.Set(""), want: `""`},
		{name: "Success: an unset String marshals to null", val: String{}, want: "null"},
		{name: "Success: a set Int marshals to its number", val: Int{}.Set(-42), want: "-42"},
		{name: "Success: an Int set to zero marshals to 0", val: Int{}.Set(0), want: "0"},
		{name: "Success: an unset Int marshals to null", val: Int{}, want: "null"},
		{name: "Success: a set Int8 marshals to its number", val: Int8{}.Set(-128), want: "-128"},
		{name: "Success: an unset Int8 marshals to null", val: Int8{}, want: "null"},
		{name: "Success: a set Int16 marshals to its number", val: Int16{}.Set(32767), want: "32767"},
		{name: "Success: an unset Int16 marshals to null", val: Int16{}, want: "null"},
		{name: "Success: a set Int32 marshals to its number", val: Int32{}.Set(-2147483648), want: "-2147483648"},
		{name: "Success: an unset Int32 marshals to null", val: Int32{}, want: "null"},
		{name: "Success: a set Int64 marshals to its number", val: Int64{}.Set(-9223372036854775808), want: "-9223372036854775808"},
		{name: "Success: an unset Int64 marshals to null", val: Int64{}, want: "null"},
		{name: "Success: a set Uint marshals to its number", val: Uint{}.Set(42), want: "42"},
		{name: "Success: a Uint set to zero marshals to 0", val: Uint{}.Set(0), want: "0"},
		{name: "Success: an unset Uint marshals to null", val: Uint{}, want: "null"},
		{name: "Success: a set Uint8 marshals to its number", val: Uint8{}.Set(255), want: "255"},
		{name: "Success: an unset Uint8 marshals to null", val: Uint8{}, want: "null"},
		{name: "Success: a set Uint16 marshals to its number", val: Uint16{}.Set(65535), want: "65535"},
		{name: "Success: an unset Uint16 marshals to null", val: Uint16{}, want: "null"},
		{name: "Success: a set Uint32 marshals to its number", val: Uint32{}.Set(4294967295), want: "4294967295"},
		{name: "Success: an unset Uint32 marshals to null", val: Uint32{}, want: "null"},
		{name: "Success: a set Uint64 marshals to its number", val: Uint64{}.Set(18446744073709551615), want: "18446744073709551615"},
		{name: "Success: an unset Uint64 marshals to null", val: Uint64{}, want: "null"},
		{name: "Success: a set Float32 marshals to its number", val: Float32{}.Set(42.5), want: "42.5"},
		{name: "Success: an unset Float32 marshals to null", val: Float32{}, want: "null"},
		{name: "Success: a set Float64 marshals to its number", val: Float64{}.Set(-42.5), want: "-42.5"},
		{name: "Success: a Float64 set to zero marshals to 0", val: Float64{}.Set(0), want: "0"},
		{name: "Success: an unset Float64 marshals to null", val: Float64{}, want: "null"},
		{
			name: "Success: a struct with every field set to its zero value marshals the zero values",
			val: stdlibConfig{
				Name:    String{}.Set(""),
				Enabled: Bool{}.Set(false),
				Retries: Int{}.Set(0),
				Offset:  Int64{}.Set(0),
				Port:    Uint16{}.Set(0),
				Ratio:   Float64{}.Set(0),
				Ports:   []Uint16{Uint16{}.Set(0)},
			},
			want: `{"name":"","enabled":false,"retries":0,"offset":0,"port":0,"ratio":0,"ports":[0]}`,
		},
		{
			name: "Success: a struct with no fields set marshals every field to null",
			val:  stdlibConfig{Ports: []Uint16{{}, Uint16{}.Set(80)}},
			want: `{"name":null,"enabled":null,"retries":null,"offset":null,"port":null,"ratio":null,"ports":[null,80]}`,
		},
		{
			name: "Success: omitzero drops unset fields",
			val:  stdlibOmitConfig{},
			want: `{}`,
		},
		{
			name: "Success: omitzero keeps fields that are set to their zero value",
			val: stdlibOmitConfig{
				Name:    String{}.Set(""),
				Enabled: Bool{}.Set(false),
				Retries: Int{}.Set(0),
				Ratio:   Float64{}.Set(0),
			},
			want: `{"name":"","enabled":false,"retries":0,"ratio":0}`,
		},
		{
			name: "Success: omitzero drops only the unset fields of a partially set struct",
			val:  stdlibOmitConfig{Retries: Int{}.Set(3)},
			want: `{"retries":3}`,
		},
	}

	for _, test := range tests {
		b, err := stdlibMarshal(test.val)
		if err != nil {
			t.Errorf("TestStdlibMarshal(%s): got err == %s, want err == nil", test.name, err)
			continue
		}
		if string(b) != test.want {
			t.Errorf("TestStdlibMarshal(%s): got %s, want %s", test.name, b, test.want)
		}
	}
}

func TestStdlibUnmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		// into is a pointer to a zero value that input is unmarshalled into.
		into any
		// want is a pointer to the value into should hold after unmarshalling.
		want    any
		wantErr bool
	}{
		{name: "Success: a JSON true decodes into a Bool", input: "true", into: new(Bool), want: ptr(Bool{}.Set(true))},
		{name: "Success: a JSON false decodes into a set Bool", input: "false", into: new(Bool), want: ptr(Bool{}.Set(false))},
		{name: "Success: a JSON null leaves a Bool unset", input: "null", into: new(Bool), want: new(Bool)},
		{name: "Error: a JSON string is not a Bool", input: `"true"`, into: new(Bool), wantErr: true},
		{name: "Success: a JSON string decodes into a String", input: `"hello"`, into: new(String), want: ptr(String{}.Set("hello"))},
		{name: "Success: an empty JSON string decodes into a set String", input: `""`, into: new(String), want: ptr(String{}.Set(""))},
		{name: "Success: a JSON null leaves a String unset", input: "null", into: new(String), want: new(String)},
		{name: "Error: a JSON number is not a String", input: "42", into: new(String), wantErr: true},
		{name: "Success: a JSON number decodes into an Int", input: "-42", into: new(Int), want: ptr(Int{}.Set(-42))},
		{name: "Success: a JSON zero decodes into a set Int", input: "0", into: new(Int), want: ptr(Int{}.Set(0))},
		{name: "Success: a JSON null leaves an Int unset", input: "null", into: new(Int), want: new(Int)},
		{name: "Error: a JSON string is not an Int", input: `"42"`, into: new(Int), wantErr: true},
		{name: "Error: a JSON number with a fraction is not an Int", input: "1.5", into: new(Int), wantErr: true},
		{name: "Error: a JSON number with an exponent is not an Int", input: "1e2", into: new(Int), wantErr: true},
		{name: "Error: a JSON number past the int64 range does not fit an Int", input: "9223372036854775808", into: new(Int), wantErr: true},
		{name: "Success: the smallest int8 decodes into an Int8", input: "-128", into: new(Int8), want: ptr(Int8{}.Set(-128))},
		{name: "Error: a JSON number past the int8 range does not fit an Int8", input: "128", into: new(Int8), wantErr: true},
		{name: "Success: the largest int16 decodes into an Int16", input: "32767", into: new(Int16), want: ptr(Int16{}.Set(32767))},
		{name: "Error: a JSON number past the int16 range does not fit an Int16", input: "32768", into: new(Int16), wantErr: true},
		{name: "Success: the smallest int32 decodes into an Int32", input: "-2147483648", into: new(Int32), want: ptr(Int32{}.Set(-2147483648))},
		{name: "Error: a JSON number past the int32 range does not fit an Int32", input: "-2147483649", into: new(Int32), wantErr: true},
		{name: "Success: the smallest int64 decodes into an Int64", input: "-9223372036854775808", into: new(Int64), want: ptr(Int64{}.Set(-9223372036854775808))},
		{name: "Error: a JSON number past the int64 range does not fit an Int64", input: "-9223372036854775809", into: new(Int64), wantErr: true},
		{name: "Success: a JSON number decodes into a Uint", input: "42", into: new(Uint), want: ptr(Uint{}.Set(42))},
		{name: "Success: a JSON zero decodes into a set Uint", input: "0", into: new(Uint), want: ptr(Uint{}.Set(0))},
		{name: "Success: a JSON null leaves a Uint unset", input: "null", into: new(Uint), want: new(Uint)},
		{name: "Error: a negative JSON number is not a Uint", input: "-1", into: new(Uint), wantErr: true},
		{name: "Error: a JSON number with a fraction is not a Uint", input: "1.5", into: new(Uint), wantErr: true},
		{name: "Error: a JSON string is not a Uint", input: `"42"`, into: new(Uint), wantErr: true},
		{name: "Success: the largest uint8 decodes into a Uint8", input: "255", into: new(Uint8), want: ptr(Uint8{}.Set(255))},
		{name: "Error: a JSON number past the uint8 range does not fit a Uint8", input: "256", into: new(Uint8), wantErr: true},
		{name: "Success: the largest uint16 decodes into a Uint16", input: "65535", into: new(Uint16), want: ptr(Uint16{}.Set(65535))},
		{name: "Error: a JSON number past the uint16 range does not fit a Uint16", input: "65536", into: new(Uint16), wantErr: true},
		{name: "Success: the largest uint32 decodes into a Uint32", input: "4294967295", into: new(Uint32), want: ptr(Uint32{}.Set(4294967295))},
		{name: "Error: a JSON number past the uint32 range does not fit a Uint32", input: "4294967296", into: new(Uint32), wantErr: true},
		{name: "Success: the largest uint64 decodes into a Uint64", input: "18446744073709551615", into: new(Uint64), want: ptr(Uint64{}.Set(18446744073709551615))},
		{name: "Error: a JSON number past the uint64 range does not fit a Uint64", input: "18446744073709551616", into: new(Uint64), wantErr: true},
		{name: "Success: a JSON number decodes into a Float32", input: "42.5", into: new(Float32), want: ptr(Float32{}.Set(42.5))},
		{name: "Success: a JSON null leaves a Float32 unset", input: "null", into: new(Float32), want: new(Float32)},
		{name: "Error: a JSON number past the float32 range does not fit a Float32", input: "1e39", into: new(Float32), wantErr: true},
		{name: "Success: a JSON number decodes into a Float64", input: "-42.5", into: new(Float64), want: ptr(Float64{}.Set(-42.5))},
		{name: "Success: a JSON number with an exponent decodes into a Float64", input: "1e2", into: new(Float64), want: ptr(Float64{}.Set(100))},
		{name: "Success: a JSON zero decodes into a set Float64", input: "0", into: new(Float64), want: ptr(Float64{}.Set(0))},
		{name: "Success: a JSON null leaves a Float64 unset", input: "null", into: new(Float64), want: new(Float64)},
		{name: "Error: a JSON number past the float64 range does not fit a Float64", input: "1e1000", into: new(Float64), wantErr: true},
		{name: "Error: a JSON string is not a Float64", input: `"42.5"`, into: new(Float64), wantErr: true},
		{
			name:  "Success: struct fields holding zero values decode as set",
			input: `{"name":"","enabled":false,"retries":0,"offset":0,"port":0,"ratio":0,"ports":[0]}`,
			into:  new(stdlibConfig),
			want: &stdlibConfig{
				Name:    String{}.Set(""),
				Enabled: Bool{}.Set(false),
				Retries: Int{}.Set(0),
				Offset:  Int64{}.Set(0),
				Port:    Uint16{}.Set(0),
				Ratio:   Float64{}.Set(0),
				Ports:   []Uint16{Uint16{}.Set(0)},
			},
		},
		{
			name:  "Success: struct fields holding null decode as unset",
			input: `{"name":null,"enabled":null,"retries":null,"offset":null,"port":null,"ratio":null,"ports":[null,80,null]}`,
			into:  new(stdlibConfig),
			want:  &stdlibConfig{Ports: []Uint16{{}, Uint16{}.Set(80), {}}},
		},
		{
			name:  "Success: struct fields missing from the JSON object stay unset",
			input: `{"retries":3,"ratio":0.5}`,
			into:  new(stdlibConfig),
			want:  &stdlibConfig{Retries: Int{}.Set(3), Ratio: Float64{}.Set(0.5)},
		},
		{
			name:    "Error: a struct field that overflows its type fails the whole object",
			input:   `{"retries":3,"port":65536,"ratio":0.5}`,
			into:    new(stdlibConfig),
			wantErr: true,
		},
		{
			name:    "Error: a slice element that overflows its type fails the whole object",
			input:   `{"ports":[80,65536]}`,
			into:    new(stdlibConfig),
			wantErr: true,
		},
	}

	for _, test := range tests {
		err := stdlibUnmarshal([]byte(test.input), test.into)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestStdlibUnmarshal(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestStdlibUnmarshal(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		if diff := pretty.Compare(test.want, test.into); diff != "" {
			t.Errorf("TestStdlibUnmarshal(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T {
	return &v
}
