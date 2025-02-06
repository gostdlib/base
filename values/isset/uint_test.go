package isset

import (
	"bytes"
	"testing"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

func TestUint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation func() Uint
		wantUint  uint
		wantIsSet bool
	}{
		{
			name: "Set Uint",
			operation: func() Uint {
				var v Uint
				return v.Set(42)
			},
			wantUint:  42,
			wantIsSet: true,
		},
		{
			name: "Unset Uint",
			operation: func() Uint {
				var v Uint
				v = v.Set(42)
				return v.Unset()
			},
			wantUint:  0, // Default zero Uint
			wantIsSet: false,
		},
		{
			name: "Default Uint",
			operation: func() Uint {
				return Uint{}
			},
			wantUint:  0, // Default zero Uint for uint
			wantIsSet: false,
		},
		{
			name: "Set the zero Uint",
			operation: func() Uint {
				var v Uint
				return v.Set(0)
			},
			wantUint:  0,
			wantIsSet: true,
		},
	}

	for _, tt := range tests {
		v := tt.operation()
		if got := v.V(); got != tt.wantUint {
			t.Errorf("TestUint(%s): V() = %v, want %v", tt.name, got, tt.wantUint)
		}
		if got := v.IsSet(); got != tt.wantIsSet {
			t.Errorf("TestUint(%s): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
		}
	}
}

func TestUintMarshalling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		initial    Uint
		jsonInput  string
		wantUint   uint
		wantIsSet  bool
		wantOutput string
	}{
		{
			name:       "Marshal set Uint",
			initial:    Uint{}.Set(42),
			jsonInput:  "",
			wantUint:   42,
			wantIsSet:  true,
			wantOutput: "42",
		},
		{
			name:       "Marshal unset Uint",
			initial:    Uint{},
			jsonInput:  "",
			wantUint:   0,
			wantIsSet:  false,
			wantOutput: "",
		},
		{
			name:       "Unmarshal set Uint",
			initial:    Uint{},
			jsonInput:  "42",
			wantUint:   42,
			wantIsSet:  true,
			wantOutput: "",
		},
		{
			name:       "Unmarshal null Uint",
			initial:    Uint{},
			jsonInput:  "null",
			wantUint:   0,
			wantIsSet:  false,
			wantOutput: "",
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
			var v Uint
			err := v.UnmarshalJSON([]byte(tt.jsonInput))
			if err != nil {
				t.Fatalf("TestMarshalling(%s)(v1) failed: %v", tt.name, err)
			}
			if got := v.V(); got != tt.wantUint {
				t.Errorf("TestMarshalling(%s)(v1): V() = %v, want %v", tt.name, got, tt.wantUint)
			}
			if got := v.IsSet(); got != tt.wantIsSet {
				t.Errorf("TestMarshalling(%s)(v1): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
			}

			// Test v2
			var v2 Uint
			dec := jsontext.NewDecoder(bytes.NewReader([]byte(tt.jsonInput)))
			err = v2.UnmarshalJSONV2(dec, json.DefaultOptionsV2())
			if err != nil {
				t.Fatalf("TestMarshalling(%s)(v2) failed: %v", tt.name, err)
			}
			if got := v2.V(); got != tt.wantUint {
				t.Errorf("TestMarshalling(%s)(v2): V() = %v, want %v", tt.name, got, tt.wantUint)
			}
			if got := v2.IsSet(); got != tt.wantIsSet {
				t.Errorf("TestMarshalling(%s)(v2): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
			}
		}
	}
}
