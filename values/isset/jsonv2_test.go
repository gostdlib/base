package isset

import (
	"testing"

	"github.com/go-json-experiment/json"
)

// TestUnmarshalJSONFrom tests decoding JSON numbers through the json v2 path. json.Unmarshal only reaches these
// methods if the types implement json.UnmarshalerFrom, so this also pins that wiring. The error cases exist
// because jsontext's Int/Uint/Float accessors report a value and an error; ignoring the error stored a
// saturated or truncated number as if it were the real one.
func TestUnmarshalJSONFrom(t *testing.T) {
	t.Parallel()

	intFrom := func(data []byte) (any, bool, error) {
		var v Int
		err := json.Unmarshal(data, &v)
		return v.V(), v.IsSet(), err
	}
	uintFrom := func(data []byte) (any, bool, error) {
		var v Uint
		err := json.Unmarshal(data, &v)
		return v.V(), v.IsSet(), err
	}
	floatFrom := func(data []byte) (any, bool, error) {
		var v Float64
		err := json.Unmarshal(data, &v)
		return v.V(), v.IsSet(), err
	}
	int8From := func(data []byte) (any, bool, error) {
		var v Int8
		err := json.Unmarshal(data, &v)
		return v.V(), v.IsSet(), err
	}
	uint16From := func(data []byte) (any, bool, error) {
		var v Uint16
		err := json.Unmarshal(data, &v)
		return v.V(), v.IsSet(), err
	}
	float32From := func(data []byte) (any, bool, error) {
		var v Float32
		err := json.Unmarshal(data, &v)
		return v.V(), v.IsSet(), err
	}

	tests := []struct {
		name      string
		input     string
		unmarshal func(data []byte) (any, bool, error)
		want      any
		wantIsSet bool
		wantErr   bool
	}{
		{
			name:      "Success: a JSON number decodes into an Int",
			input:     "42",
			unmarshal: intFrom,
			want:      42,
			wantIsSet: true,
		},
		{
			name:      "Success: a JSON null leaves an Int unset",
			input:     "null",
			unmarshal: intFrom,
			want:      0,
			wantIsSet: false,
		},
		{
			name:      "Error: a JSON number past the int64 range does not fit an Int",
			input:     "9223372036854775808",
			unmarshal: intFrom,
			wantErr:   true,
		},
		{
			name:      "Error: a JSON number with a fraction is not an Int",
			input:     "1.5",
			unmarshal: intFrom,
			wantErr:   true,
		},
		{
			name:      "Success: a JSON number decodes into a Uint",
			input:     "42",
			unmarshal: uintFrom,
			want:      uint(42),
			wantIsSet: true,
		},
		{
			name:      "Error: a JSON number past the uint64 range does not fit a Uint",
			input:     "18446744073709551616",
			unmarshal: uintFrom,
			wantErr:   true,
		},
		{
			name:      "Error: a negative JSON number is not a Uint",
			input:     "-1",
			unmarshal: uintFrom,
			wantErr:   true,
		},
		{
			name:      "Success: a JSON number decodes into a Float64",
			input:     "42.5",
			unmarshal: floatFrom,
			want:      42.5,
			wantIsSet: true,
		},
		{
			name:      "Error: a JSON number past the float64 range does not fit a Float64",
			input:     "1e1000",
			unmarshal: floatFrom,
			wantErr:   true,
		},
		{
			name:      "Success: a JSON number decodes into an Int8",
			input:     "-128",
			unmarshal: int8From,
			want:      int8(-128),
			wantIsSet: true,
		},
		{
			name:      "Error: a JSON number past the int8 range does not fit an Int8",
			input:     "200",
			unmarshal: int8From,
			wantErr:   true,
		},
		{
			name:      "Success: a JSON number decodes into a Uint16",
			input:     "65535",
			unmarshal: uint16From,
			want:      uint16(65535),
			wantIsSet: true,
		},
		{
			name:      "Error: a JSON number past the uint16 range does not fit a Uint16",
			input:     "70000",
			unmarshal: uint16From,
			wantErr:   true,
		},
		{
			name:      "Success: a JSON number decodes into a Float32",
			input:     "42.5",
			unmarshal: float32From,
			want:      float32(42.5),
			wantIsSet: true,
		},
		{
			name:      "Error: a JSON number past the float32 range does not fit a Float32",
			input:     "1e39",
			unmarshal: float32From,
			wantErr:   true,
		},
	}

	for _, test := range tests {
		got, isSet, err := test.unmarshal([]byte(test.input))
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestUnmarshalJSONFrom(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestUnmarshalJSONFrom(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		if got != test.want {
			t.Errorf("TestUnmarshalJSONFrom(%s): V() = %v, want %v", test.name, got, test.want)
		}
		if isSet != test.wantIsSet {
			t.Errorf("TestUnmarshalJSONFrom(%s): IsSet() = %v, want %v", test.name, isSet, test.wantIsSet)
		}
	}
}

// TestMarshalJSONTo tests encoding through the json v2 path. json.Marshal only reaches these methods if the
// types implement json.MarshalerTo.
func TestMarshalJSONTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  any
		want string
	}{
		{
			name: "Success: a set Int marshals to its number",
			val:  Int{}.Set(42),
			want: "42",
		},
		{
			name: "Success: an unset Int marshals to null",
			val:  Int{},
			want: "null",
		},
		{
			name: "Success: a set Uint16 marshals to its number",
			val:  Uint16{}.Set(65535),
			want: "65535",
		},
		{
			name: "Success: a set Float64 marshals to its number",
			val:  Float64{}.Set(42.5),
			want: "42.5",
		},
		{
			name: "Success: a set String marshals to its string",
			val:  String{}.Set("hello"),
			want: `"hello"`,
		},
		{
			name: "Success: a set Bool marshals to its boolean",
			val:  Bool{}.Set(true),
			want: "true",
		},
	}

	for _, test := range tests {
		b, err := json.Marshal(test.val)
		if err != nil {
			t.Errorf("TestMarshalJSONTo(%s): got err == %s, want err == nil", test.name, err)
			continue
		}
		if string(b) != test.want {
			t.Errorf("TestMarshalJSONTo(%s): got %s, want %s", test.name, b, test.want)
		}
	}
}
