package isset

import (
	"bytes"
	"testing"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

func TestBool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation func() Bool
		wantBool  bool
		wantIsSet bool
	}{
		{
			name: "Set Bool",
			operation: func() Bool {
				var v Bool
				return v.Set(true)
			},
			wantBool:  true,
			wantIsSet: true,
		},
		{
			name: "Unset Bool",
			operation: func() Bool {
				var v Bool
				v = v.Set(true)
				return v.Unset()
			},
			wantBool:  false, // Default zero Bool
			wantIsSet: false,
		},
		{
			name: "Default Bool",
			operation: func() Bool {
				return Bool{}
			},
			wantBool:  false, // Default zero Bool for int
			wantIsSet: false,
		},
		{
			name: "Set the zero Bool",
			operation: func() Bool {
				var v Bool
				return v.Set(false)
			},
			wantBool:  false,
			wantIsSet: true,
		},
	}

	for _, tt := range tests {
		v := tt.operation()
		if got := v.V(); got != tt.wantBool {
			t.Errorf("TestBool(%s): V() = %v, want %v", tt.name, got, tt.wantBool)
		}
		if got := v.IsSet(); got != tt.wantIsSet {
			t.Errorf("TestBool(%s): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
		}
	}
}

func TestBoolMarshalling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		initial    Bool
		jsonInput  string
		wantBool   bool
		wantIsSet  bool
		wantOutput string
	}{
		{
			name:       "Marshal set Bool",
			initial:    Bool{}.Set(true),
			jsonInput:  "",
			wantBool:   true,
			wantIsSet:  true,
			wantOutput: "true",
		},
		{
			name:       "Marshal unset Bool",
			initial:    Bool{},
			jsonInput:  "",
			wantBool:   false,
			wantIsSet:  false,
			wantOutput: "",
		},
		{
			name:       "Unmarshal set Bool",
			initial:    Bool{},
			jsonInput:  "true",
			wantBool:   true,
			wantIsSet:  true,
			wantOutput: "true",
		},
		{
			name:       "Unmarshal null Bool",
			initial:    Bool{},
			jsonInput:  "null",
			wantBool:   false,
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
			var v Bool
			err := v.UnmarshalJSON([]byte(tt.jsonInput))
			if err != nil {
				t.Fatalf("TestMarshalling(%s)(v1) failed: %v", tt.name, err)
			}
			if got := v.V(); got != tt.wantBool {
				t.Errorf("TestMarshalling(%s)(v1): V() = %v, want %v", tt.name, got, tt.wantBool)
			}
			if got := v.IsSet(); got != tt.wantIsSet {
				t.Errorf("TestMarshalling(%s)(v1): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
			}

			// Test v2
			var v2 Bool
			dec := jsontext.NewDecoder(bytes.NewReader([]byte(tt.jsonInput)))
			err = v2.UnmarshalJSONV2(dec, json.DefaultOptionsV2())
			if err != nil {
				t.Fatalf("TestMarshalling(%s)(v2) failed: %v", tt.name, err)
			}
			if got := v2.V(); got != tt.wantBool {
				t.Errorf("TestMarshalling(%s)(v2): V() = %v, want %v", tt.name, got, tt.wantBool)
			}
			if got := v2.IsSet(); got != tt.wantIsSet {
				t.Errorf("TestMarshalling(%s)(v2): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
			}
		}
	}
}
