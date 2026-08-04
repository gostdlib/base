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
			name: "Success: single type",
			cfg:  config{types: []string{"Color"}},
		},
		{
			name: "Success: multiple types",
			cfg:  config{types: []string{"Color", "ID"}},
		},
		{
			name: "Success: one type with values",
			cfg:  config{types: []string{"Color"}, values: []string{"blue", "yellow"}},
		},
		{
			name:    "Error: no types",
			cfg:     config{},
			wantErr: true,
		},
		{
			name:    "Error: values with multiple types",
			cfg:     config{types: []string{"Color", "ID"}, values: []string{"blue"}},
			wantErr: true,
		},
		{
			name:    "Error: type is not a valid identifier",
			cfg:     config{types: []string{"Co-lor"}},
			wantErr: true,
		},
		{
			name:    "Error: duplicate type",
			cfg:     config{types: []string{"Color", "Color"}},
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

func TestAnalyze(t *testing.T) {
	tests := []struct {
		name    string
		dir     string // package directory; defaults to testdata/data.
		cfg     config
		wantPkg string
		want    []setDef
		wantErr bool
	}{
		{
			name:    "Success: package that does not compile yet because it uses the set",
			dir:     "testdata/bootstrap",
			cfg:     config{types: []string{"Color"}},
			wantPkg: "bootstrap",
			want:    []setDef{{Type: "Color", Elems: "Blue, Red", Doc: "ColorSet is an immutable set holding the constant values of type Color."}},
		},
		{
			name:    "Success: constants of a string type, including unexported ones",
			cfg:     config{types: []string{"Color"}},
			wantPkg: "data",
			want:    []setDef{{Type: "Color", Elems: "Blue, Red, green", Doc: "ColorSet is an immutable set holding the constant values of type Color."}},
		},
		{
			name:    "Success: unexported type",
			cfg:     config{types: []string{"hue"}},
			wantPkg: "data",
			want:    []setDef{{Type: "hue", Elems: "hueA", Doc: "hueSet is an immutable set holding the constant values of type hue."}},
		},
		{
			name:    "Success: constants of an int type",
			cfg:     config{types: []string{"ID"}},
			wantPkg: "data",
			want:    []setDef{{Type: "ID", Elems: "One, Zero", Doc: "IDSet is an immutable set holding the constant values of type ID."}},
		},
		{
			name:    "Success: multiple types",
			cfg:     config{types: []string{"Color", "ID"}},
			wantPkg: "data",
			want: []setDef{
				{Type: "Color", Elems: "Blue, Red, green", Doc: "ColorSet is an immutable set holding the constant values of type Color."},
				{Type: "ID", Elems: "One, Zero", Doc: "IDSet is an immutable set holding the constant values of type ID."},
			},
		},
		{
			name:    "Success: explicit string values",
			cfg:     config{types: []string{"Color"}, values: []string{"blue", "yellow"}},
			wantPkg: "data",
			want:    []setDef{{Type: "Color", Elems: `"blue", "yellow"`, Doc: "ColorSet is an immutable set of Color values."}},
		},
		{
			name:    "Success: explicit int values",
			cfg:     config{types: []string{"ID"}, values: []string{"1", "2"}},
			wantPkg: "data",
			want:    []setDef{{Type: "ID", Elems: "1, 2", Doc: "IDSet is an immutable set of ID values."}},
		},
		{
			name:    "Success: explicit float values",
			cfg:     config{types: []string{"Bare"}, values: []string{"1.5", "2"}},
			wantPkg: "data",
			want:    []setDef{{Type: "Bare", Elems: "1.5, 2", Doc: "BareSet is an immutable set of Bare values."}},
		},
		{
			name:    "Error: type not declared in package",
			cfg:     config{types: []string{"Missing"}},
			wantErr: true,
		},
		{
			name:    "Error: name is not a type",
			cfg:     config{types: []string{"NotAType"}},
			wantErr: true,
		},
		{
			name:    "Error: underlying type is not allowed",
			cfg:     config{types: []string{"Sliced"}},
			wantErr: true,
		},
		{
			name:    "Error: type has no constants",
			cfg:     config{types: []string{"Bare"}},
			wantErr: true,
		},
		{
			name:    "Error: value is not valid for an int type",
			cfg:     config{types: []string{"ID"}, values: []string{"abc"}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		dir := test.dir
		if dir == "" {
			dir = "testdata/data"
		}
		pkg, sets, err := analyze(dir, test.cfg)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestAnalyze(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestAnalyze(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
		if pkg != test.wantPkg {
			t.Errorf("TestAnalyze(%s): got pkg == %q, want %q", test.name, pkg, test.wantPkg)
		}
		if diff := pretty.Compare(test.want, sets); diff != "" {
			t.Errorf("TestAnalyze(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestGenerate(t *testing.T) {
	tests := []struct {
		name string
		cfg  config
		sets []setDef
		want string
	}{
		{
			name: "Success: constants of one type",
			cfg:  config{pkg: "paint", args: []string{"-t", "Color"}},
			sets: []setDef{{Type: "Color", Elems: "Blue, Red", Doc: "ColorSet is an immutable set holding the constant values of type Color."}},
			want: colorGolden,
		},
		{
			name: "Success: explicit values",
			cfg:  config{pkg: "paint", args: []string{"-t", "Color", "-v", "blue,yellow"}},
			sets: []setDef{{Type: "Color", Elems: `"blue", "yellow"`, Doc: "ColorSet is an immutable set of Color values."}},
			want: valuesGolden,
		},
		{
			name: "Success: multiple types in one file",
			cfg:  config{pkg: "paint", args: []string{"-t", "Color,ID"}},
			sets: []setDef{
				{Type: "Color", Elems: "Blue, Red", Doc: "ColorSet is an immutable set holding the constant values of type Color."},
				{Type: "ID", Elems: "One, Zero", Doc: "IDSet is an immutable set holding the constant values of type ID."},
			},
			want: multiGolden,
		},
	}

	for _, test := range tests {
		got, err := generate(test.cfg, test.sets)
		if err != nil {
			t.Errorf("TestGenerate(%s): got err == %s, want err == nil", test.name, err)
			continue
		}
		if diff := pretty.Compare(test.want, string(got)); diff != "" {
			t.Errorf("TestGenerate(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

const colorGolden = `// Code generated by "sets -t Color"; DO NOT EDIT.

package paint

import "github.com/gostdlib/base/values/immutable"

// ColorSet is an immutable set holding the constant values of type Color.
var ColorSet = immutable.NewSet([]Color{Blue, Red})
`

const valuesGolden = `// Code generated by "sets -t Color -v blue,yellow"; DO NOT EDIT.

package paint

import "github.com/gostdlib/base/values/immutable"

// ColorSet is an immutable set of Color values.
var ColorSet = immutable.NewSet([]Color{"blue", "yellow"})
`

const multiGolden = `// Code generated by "sets -t Color,ID"; DO NOT EDIT.

package paint

import "github.com/gostdlib/base/values/immutable"

// ColorSet is an immutable set holding the constant values of type Color.
var ColorSet = immutable.NewSet([]Color{Blue, Red})

// IDSet is an immutable set holding the constant values of type ID.
var IDSet = immutable.NewSet([]ID{One, Zero})
`
