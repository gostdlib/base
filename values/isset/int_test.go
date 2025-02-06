package isset

import (
	"bytes"
	"testing"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

func TestInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation func() Int
		wantInt   int
		wantIsSet bool
	}{
		{
			name: "Set Int",
			operation: func() Int {
				var v Int
				return v.Set(42)
			},
			wantInt:   42,
			wantIsSet: true,
		},
		{
			name: "Unset Int",
			operation: func() Int {
				var v Int
				v = v.Set(42)
				return v.Unset()
			},
			wantInt:   0, // Default zero Int
			wantIsSet: false,
		},
		{
			name: "Default Int",
			operation: func() Int {
				return Int{}
			},
			wantInt:   0, // Default zero Int for int
			wantIsSet: false,
		},
		{
			name: "Set the zero Int",
			operation: func() Int {
				var v Int
				return v.Set(0)
			},
			wantInt:   0,
			wantIsSet: true,
		},
	}

	for _, tt := range tests {
		v := tt.operation()
		if got := v.V(); got != tt.wantInt {
			t.Errorf("TestInt(%s): V() = %v, want %v", tt.name, got, tt.wantInt)
		}
		if got := v.IsSet(); got != tt.wantIsSet {
			t.Errorf("TestInt(%s): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
		}
	}
}

func TestIntMarshalling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		initial    Int
		jsonInput  string
		wantInt    int
		wantIsSet  bool
		wantOutput string
	}{
		{
			name:       "Marshal set Int",
			initial:    Int{}.Set(42),
			jsonInput:  "",
			wantInt:    42,
			wantIsSet:  true,
			wantOutput: "42",
		},
		{
			name:       "Marshal unset Int",
			initial:    Int{},
			jsonInput:  "",
			wantInt:    0,
			wantIsSet:  false,
			wantOutput: "",
		},
		{
			name:       "Unmarshal set Int",
			initial:    Int{},
			jsonInput:  "42",
			wantInt:    42,
			wantIsSet:  true,
			wantOutput: "",
		},
		{
			name:       "Unmarshal null Int",
			initial:    Int{},
			jsonInput:  "null",
			wantInt:    0,
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
			var v Int
			err := v.UnmarshalJSON([]byte(tt.jsonInput))
			if err != nil {
				t.Fatalf("TestMarshalling(%s)(v1) failed: %v", tt.name, err)
			}
			if got := v.V(); got != tt.wantInt {
				t.Errorf("TestMarshalling(%s)(v1): V() = %v, want %v", tt.name, got, tt.wantInt)
			}
			if got := v.IsSet(); got != tt.wantIsSet {
				t.Errorf("TestMarshalling(%s)(v1): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
			}

			// Test v2
			var v2 Int
			dec := jsontext.NewDecoder(bytes.NewReader([]byte(tt.jsonInput)))
			err = v2.UnmarshalJSONV2(dec, json.DefaultOptionsV2())
			if err != nil {
				t.Fatalf("TestMarshalling(%s)(v2) failed: %v", tt.name, err)
			}
			if got := v2.V(); got != tt.wantInt {
				t.Errorf("TestMarshalling(%s)(v2): V() = %v, want %v", tt.name, got, tt.wantInt)
			}
			if got := v2.IsSet(); got != tt.wantIsSet {
				t.Errorf("TestMarshalling(%s)(v2): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
			}
		}
	}
}
