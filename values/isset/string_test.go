package isset

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

func TestString(t *testing.T) {
	t.Parallel()

	s := "hello"

	tests := []struct {
		name      string
		operation func() String
		wantStr   string
		wantIsSet bool
	}{
		{
			name: "Set String",
			operation: func() String {
				var v String
				return v.Set(s)
			},
			wantStr:   s,
			wantIsSet: true,
		},
		{
			name: "Unset String",
			operation: func() String {
				var v String
				v = v.Set(s)
				return v.Unset()
			},
			wantStr:   "", // Default zero String
			wantIsSet: false,
		},
		{
			name: "Default String",
			operation: func() String {
				return String{}
			},
			wantStr:   "", // Default zero String
			wantIsSet: false,
		},
		{
			name: "Set the zero String",
			operation: func() String {
				var v String
				return v.Set("")
			},
			wantStr:   "",
			wantIsSet: true,
		},
	}

	for _, tt := range tests {
		v := tt.operation()
		if got := v.V(); got != tt.wantStr {
			t.Errorf("TestBool(%s): V() = %v, want %v", tt.name, got, tt.wantStr)
		}
		if got := v.IsSet(); got != tt.wantIsSet {
			t.Errorf("TestBool(%s): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
		}
	}
}

func TestStringMarshalling(t *testing.T) {
	t.Parallel()

	s := "hello"

	tests := []struct {
		name       string
		initial    String
		jsonInput  string
		wantStr    string
		wantIsSet  bool
		wantOutput string
	}{
		{
			name:       "Marshal set String",
			initial:    String{}.Set(s),
			jsonInput:  "",
			wantStr:    s,
			wantIsSet:  true,
			wantOutput: fmt.Sprintf("%q", s),
		},
		{
			name:       "Marshal unset String",
			initial:    String{},
			jsonInput:  "",
			wantStr:    "",
			wantIsSet:  false,
			wantOutput: "",
		},
		{
			name:       "Unmarshal set String",
			initial:    String{},
			jsonInput:  s,
			wantStr:    s,
			wantIsSet:  true,
			wantOutput: "",
		},
		{
			name:       "Unmarshal null String",
			initial:    String{},
			jsonInput:  "null",
			wantStr:    "",
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
			var v String
			input := tt.jsonInput
			if input != "null" {
				input = fmt.Sprintf("%q", input)
			}
			err := v.UnmarshalJSON([]byte(input))
			if err != nil {
				t.Fatalf("TestMarshalling(%s)(v1) failed: %v", tt.name, err)
			}
			if got := v.V(); got != tt.wantStr {
				t.Errorf("TestMarshalling(%s)(v1): V() = %v, want %v", tt.name, got, tt.wantStr)
			}
			if got := v.IsSet(); got != tt.wantIsSet {
				t.Errorf("TestMarshalling(%s)(v1): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
			}

			// Test v2
			var v2 String
			dec := jsontext.NewDecoder(bytes.NewReader([]byte(input)))
			err = v2.UnmarshalJSONV2(dec, json.DefaultOptionsV2())
			if err != nil {
				t.Fatalf("TestMarshalling(%s)(v2) failed: %v", tt.name, err)
			}
			if got := v2.V(); got != tt.wantStr {
				t.Errorf("TestMarshalling(%s)(v2): V() = %v, want %v", tt.name, got, tt.wantStr)
			}
			if got := v2.IsSet(); got != tt.wantIsSet {
				t.Errorf("TestMarshalling(%s)(v2): IsSet() = %v, want %v", tt.name, got, tt.wantIsSet)
			}
		}
	}
}
