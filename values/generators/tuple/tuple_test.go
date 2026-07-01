package main

import (
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config
		wantErr bool
	}{
		{
			name: "Success: unexported name with two positional fields",
			cfg:  config{name: "lastFirst", fields: []field{{Field: "v0", Type: "string"}, {Field: "v1", Type: "string"}}},
		},
		{
			name: "Success: exported name with one named field",
			cfg:  config{name: "LastFirst", fields: []field{{Field: "last", Type: "string"}}},
		},
		{
			name:    "Error: missing name",
			cfg:     config{fields: []field{{Field: "v0", Type: "string"}}},
			wantErr: true,
		},
		{
			name:    "Error: name is not a valid identifier",
			cfg:     config{name: "1last", fields: []field{{Field: "v0", Type: "string"}}},
			wantErr: true,
		},
		{
			name:    "Error: missing fields",
			cfg:     config{name: "lastFirst"},
			wantErr: true,
		},
		{
			name:    "Error: field has an empty type",
			cfg:     config{name: "lastFirst", fields: []field{{Field: "v0", Type: ""}}},
			wantErr: true,
		},
		{
			name:    "Error: duplicate field name",
			cfg:     config{name: "lastFirst", fields: []field{{Field: "last", Type: "string"}, {Field: "last", Type: "string"}}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		err := test.cfg.validate()
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestValidate(%s): got err == nil, want err != nil", test.name)
		case err != nil && !test.wantErr:
			t.Errorf("TestValidate(%s): got err == %s, want err == nil", test.name, err)
		}
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantName   string
		wantFields []field
		wantErr    bool
	}{
		{
			name:     "Success: positional fields get V<n> accessors",
			args:     []string{"lastFirst", "string,", "string"},
			wantName: "lastFirst",
			wantFields: []field{
				{Field: "v0", Accessor: "V0", Type: "string", Ordinal: "first"},
				{Field: "v1", Accessor: "V1", Type: "string", Ordinal: "second"},
			},
		},
		{
			name:     "Success: named fields get exported accessors",
			args:     []string{"lastFirst", "last:string,", "first:string"},
			wantName: "lastFirst",
			wantFields: []field{
				{Field: "last", Accessor: "Last", Type: "string", Ordinal: "first"},
				{Field: "first", Accessor: "First", Type: "string", Ordinal: "second"},
			},
		},
		{
			name:     "Success: positional and named fields may be mixed",
			args:     []string{"mix", "string,", "count:int"},
			wantName: "mix",
			wantFields: []field{
				{Field: "v0", Accessor: "V0", Type: "string", Ordinal: "first"},
				{Field: "count", Accessor: "Count", Type: "int", Ordinal: "second"},
			},
		},
		{
			name:     "Success: func type field with commas is not split",
			args:     []string{"Pair", "func(int, string), string"},
			wantName: "Pair",
			wantFields: []field{
				{Field: "v0", Accessor: "V0", Type: "func(int, string)", Ordinal: "first"},
				{Field: "v1", Accessor: "V1", Type: "string", Ordinal: "second"},
			},
		},
		{
			name:     "Success: generic type field with commas is not split",
			args:     []string{"Pair", "iter.Seq2[string, int], string"},
			wantName: "Pair",
			wantFields: []field{
				{Field: "v0", Accessor: "V0", Type: "iter.Seq2[string, int]", Ordinal: "first"},
				{Field: "v1", Accessor: "V1", Type: "string", Ordinal: "second"},
			},
		},
		{
			name:     "Success: named field with a generic type containing commas",
			args:     []string{"Pair", "seq:iter.Seq2[string, int], count:int"},
			wantName: "Pair",
			wantFields: []field{
				{Field: "seq", Accessor: "Seq", Type: "iter.Seq2[string, int]", Ordinal: "first"},
				{Field: "count", Accessor: "Count", Type: "int", Ordinal: "second"},
			},
		},
		{
			name:     "Success: positional struct type with a tag containing a colon",
			args:     []string{"Tagged", `struct{ X string ` + "`json:\"x\"`" + ` }, int`},
			wantName: "Tagged",
			wantFields: []field{
				{Field: "v0", Accessor: "V0", Type: "struct{ X string `json:\"x\"` }", Ordinal: "first"},
				{Field: "v1", Accessor: "V1", Type: "int", Ordinal: "second"},
			},
		},
		{
			name:     "Success: named struct type with a tag containing a colon",
			args:     []string{"Tagged", `row:struct{ X string ` + "`json:\"x\"`" + ` }, count:int`},
			wantName: "Tagged",
			wantFields: []field{
				{Field: "row", Accessor: "Row", Type: "struct{ X string `json:\"x\"` }", Ordinal: "first"},
				{Field: "count", Accessor: "Count", Type: "int", Ordinal: "second"},
			},
		},
		{
			name:    "Error: no arguments",
			args:    nil,
			wantErr: true,
		},
		{
			name:    "Error: named field name is not a valid identifier",
			args:    []string{"lastFirst", "1last:string"},
			wantErr: true,
		},
	}

	for _, test := range tests {
		gotName, gotFields, err := parseArgs(test.args)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestParseArgs(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestParseArgs(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
		if gotName != test.wantName {
			t.Errorf("TestParseArgs(%s): got name == %q, want %q", test.name, gotName, test.wantName)
		}
		if diff := pretty.Compare(test.wantFields, gotFields); diff != "" {
			t.Errorf("TestParseArgs(%s): fields -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestGenerate(t *testing.T) {
	tests := []struct {
		name string
		cfg  config
		want string
	}{
		{
			name: "Success: positional fields with preamble",
			cfg: config{
				pkg:      "person",
				name:     "lastFirst",
				preamble: true,
				fields: []field{
					{Field: "v0", Accessor: "V0", Type: "string", Ordinal: "first"},
					{Field: "v1", Accessor: "V1", Type: "string", Ordinal: "second"},
				},
				args: []string{"-p", "lastFirst", "string,", "string"},
			},
			want: positionalGolden,
		},
		{
			name: "Success: named fields without preamble",
			cfg: config{
				name: "lastFirst",
				fields: []field{
					{Field: "last", Accessor: "Last", Type: "string", Ordinal: "first"},
					{Field: "first", Accessor: "First", Type: "string", Ordinal: "second"},
				},
			},
			want: namedGolden,
		},
	}

	for _, test := range tests {
		got, err := generate(test.cfg)
		if err != nil {
			t.Errorf("TestGenerate(%s): got err == %s, want err == nil", test.name, err)
			continue
		}
		if diff := pretty.Compare(test.want, string(got)); diff != "" {
			t.Errorf("TestGenerate(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

const positionalGolden = `// Code generated by "tuple -p lastFirst string, string"; DO NOT EDIT.

package person

import "fmt"

// lastFirst is a tuple holding 2 values: v0 string, v1 string.
type lastFirst struct {
	v0 string
	v1 string
}

// NewlastFirst creates a new lastFirst tuple with the given values.
func NewlastFirst(v0 string, v1 string) lastFirst {
	return lastFirst{v0: v0, v1: v1}
}

// V0 returns the first value of the tuple.
func (t lastFirst) V0() string {
	return t.v0
}

// V1 returns the second value of the tuple.
func (t lastFirst) V1() string {
	return t.v1
}

// Len returns the number of values in the tuple.
func (t lastFirst) Len() int {
	return 2
}

// String implements fmt.Stringer.
func (t lastFirst) String() string {
	return fmt.Sprintf("(%v, %v)", t.v0, t.v1)
}
`

const namedGolden = `// lastFirst is a tuple holding 2 values: last string, first string.
type lastFirst struct {
	last  string
	first string
}

// NewlastFirst creates a new lastFirst tuple with the given values.
func NewlastFirst(last string, first string) lastFirst {
	return lastFirst{last: last, first: first}
}

// Last returns the first value of the tuple.
func (t lastFirst) Last() string {
	return t.last
}

// First returns the second value of the tuple.
func (t lastFirst) First() string {
	return t.first
}

// Len returns the number of values in the tuple.
func (t lastFirst) Len() int {
	return 2
}

// String implements fmt.Stringer.
func (t lastFirst) String() string {
	return fmt.Sprintf("(%v, %v)", t.last, t.first)
}
`
