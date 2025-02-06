package isset

import (
	"bytes"
	"testing"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

func TestFloat64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		operation   func() Float64
		wantFloat64 float64
		wantIsSet   bool
	}{
		{
			name: "Set Float64",
			operation: func() Float64 {
				var v Float64
				return v.Set(42)
			},
			wantFloat64: 42,
			wantIsSet:   true,
		},
		{
			name: "Unset Float64",
			operation: func() Float64 {
				var v Float64
				v = v.Set(42)
				return v.Unset()
			},
			wantFloat64: 0, // Default zero Float64
			wantIsSet:   false,
		},
		{
			name: "Default Float64",
			operation: func() Float64 {
				return Float64{}
			},
			wantFloat64: 0, // Default zero Float64 for float64
			wantIsSet:   false,
		},
		{
			name: "Set the zero Float64",
			operation: func() Float64 {
				var v Float64
				return v.Set(0)
			},
			wantFloat64: 0,
			wantIsSet:   true,
		},
	}

	for _, tt := range tests {
		v := tt.operation()
		if got := v.V(); got != tt.wantFloat64 {
			t.Errorf("TestFloat64(%s): V() = %v, want %v", tt.name, got, tt.wantFloat64)
		}
		if got := v.IsSet(); got != tt.wantIsSet {
			t.Errorf("TestFloat64(%s): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
		}
	}
}

func TestFloat64Marshalling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		initial     Float64
		jsonInput   string
		wantFloat64 float64
		wantIsSet   bool
		wantOutput  string
	}{
		{
			name:        "Marshal set Float64",
			initial:     Float64{}.Set(42),
			jsonInput:   "",
			wantFloat64: 42,
			wantIsSet:   true,
			wantOutput:  "42",
		},
		{
			name:        "Marshal unset Float64",
			initial:     Float64{},
			jsonInput:   "",
			wantFloat64: 0,
			wantIsSet:   false,
			wantOutput:  "",
		},
		{
			name:        "Unmarshal set Float64",
			initial:     Float64{},
			jsonInput:   "42",
			wantFloat64: 42,
			wantIsSet:   true,
			wantOutput:  "",
		},
		{
			name:        "Unmarshal null Float64",
			initial:     Float64{},
			jsonInput:   "null",
			wantFloat64: 0,
			wantIsSet:   false,
			wantOutput:  "",
		},
	}

	for _, tt := range tests {
		if tt.jsonInput == "" {
			// Test MarshalJSON
			jsonBytes, err := tt.initial.MarshalJSON()
			if err != nil {
				t.Fatalf("TestMarshalling(%s) failed: %v", tt.name, err)
			}
			gotOutput := string(jsonBytes)
			if gotOutput != tt.wantOutput {
				t.Errorf("TestMarshalling(%s) = %v, want %v", tt.name, gotOutput, tt.wantOutput)
			}
		} else {
			// Test UnmarshalJSON
			var v Float64
			err := v.UnmarshalJSON([]byte(tt.jsonInput))
			if err != nil {
				t.Fatalf("TestMarshalling(%s)(v1) failed: %v", tt.name, err)
			}
			if got := v.V(); got != tt.wantFloat64 {
				t.Errorf("TestMarshalling(%s)(v1): V() = %v, want %v", tt.name, got, tt.wantFloat64)
			}
			if got := v.IsSet(); got != tt.wantIsSet {
				t.Errorf("TestMarshalling(%s)(v1): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
			}

			// Test v2
			var v2 Float64
			dec := jsontext.NewDecoder(bytes.NewReader([]byte(tt.jsonInput)))
			err = v2.UnmarshalJSONV2(dec, json.DefaultOptionsV2())
			if err != nil {
				t.Fatalf("TestMarshalling(%s)(v2) failed: %v", tt.name, err)
			}
			if got := v2.V(); got != tt.wantFloat64 {
				t.Errorf("TestMarshalling(%s)(v2): V() = %v, want %v", tt.name, got, tt.wantFloat64)
			}
			if got := v2.IsSet(); got != tt.wantIsSet {
				t.Errorf("TestMarshalling(%s)(v2): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
			}
		}
	}
}
