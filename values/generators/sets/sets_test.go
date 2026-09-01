package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

// update rewrites the golden files from the generator's current output instead of comparing against them. Run
// "go test ./values/generators/sets/ -update" after an intended change to the generator, then read the diff before
// committing it. The package has to be named: a wider pattern passes -update to test binaries that never declared it.
var update = flag.Bool("update", false, "rewrite the testdata golden files instead of comparing against them")

// sourceLines splits generated source for pretty.Compare, which renders a whole file as one escaped string but a
// slice of lines as a readable per-line diff.
func sourceLines(s string) []string {
	return strings.Split(s, "\n")
}

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
			cfg:  config{types: []string{"Color"}, useValues: true, values: []string{"blue", "yellow"}},
		},
		{
			name:    "Error: no types",
			cfg:     config{},
			wantErr: true,
		},
		{
			name:    "Error: values with multiple types",
			cfg:     config{types: []string{"Color", "ID"}, useValues: true, values: []string{"blue"}},
			wantErr: true,
		},
		{
			name:    "Error: type is not a valid identifier",
			cfg:     config{types: []string{"Co-lor"}},
			wantErr: true,
		},
		{
			name: "Success: one type with an empty value",
			cfg:  config{types: []string{"Color"}, useValues: true, values: []string{""}},
		},
		{
			name:    "Error: duplicate type",
			cfg:     config{types: []string{"Color", "Color"}},
			wantErr: true,
		},
		{
			name:    "Error: values given without -v",
			cfg:     config{types: []string{"Color"}, values: []string{"blue"}},
			wantErr: true,
		},
		{
			name:    "Error: -v given with no values",
			cfg:     config{types: []string{"Color"}, useValues: true},
			wantErr: true,
		},
		{
			name:    "Error: duplicate value",
			cfg:     config{types: []string{"Color"}, useValues: true, values: []string{"blue", "blue"}},
			wantErr: true,
		},
		{
			name:    "Error: -v holding only separators, which is a duplicated empty value",
			cfg:     config{types: []string{"Color"}, useValues: true, values: []string{"", ""}},
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
			cfg:     config{types: []string{"Color"}, useValues: true, values: []string{"blue", "yellow"}},
			wantPkg: "data",
			want:    []setDef{{Type: "Color", Elems: `"blue", "yellow"`, Doc: "ColorSet is an immutable set of Color values."}},
		},
		{
			name:    "Success: explicit int values",
			cfg:     config{types: []string{"ID"}, useValues: true, values: []string{"1", "2"}},
			wantPkg: "data",
			want:    []setDef{{Type: "ID", Elems: "1, 2", Doc: "IDSet is an immutable set of ID values."}},
		},
		{
			name:    "Success: explicit float values",
			cfg:     config{types: []string{"Bare"}, useValues: true, values: []string{"1.5", "2"}},
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
			name:    "Success: explicit values including the empty string",
			cfg:     config{types: []string{"Color"}, useValues: true, values: []string{"", "blue"}},
			wantPkg: "data",
			want:    []setDef{{Type: "Color", Elems: `"", "blue"`, Doc: "ColorSet is an immutable set of Color values."}},
		},
		{
			name:    "Error: value is not valid for an int type",
			cfg:     config{types: []string{"ID"}, useValues: true, values: []string{"abc"}},
			wantErr: true,
		},
		{
			name:    "Error: value is infinity for a float type",
			cfg:     config{types: []string{"Bare"}, useValues: true, values: []string{"Inf"}},
			wantErr: true,
		},
		{
			name:    "Error: value is NaN for a float type",
			cfg:     config{types: []string{"Bare"}, useValues: true, values: []string{"NaN"}},
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

func TestSplitValues(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "Success: a list of values",
			in:   "blue,yellow",
			want: []string{"blue", "yellow"},
		},
		{
			name: "Success: surrounding spaces are trimmed",
			in:   " blue , yellow ",
			want: []string{"blue", "yellow"},
		},
		{
			name: "Success: a leading empty entry is kept as the empty value",
			in:   ",blue",
			want: []string{"", "blue"},
		},
		{
			name: "Success: an empty list is one empty value, not no values",
			in:   "",
			want: []string{""},
		},
		{
			name: "Success: separators alone are empty values, not nothing",
			in:   ",",
			want: []string{"", ""},
		},
	}

	for _, test := range tests {
		got := splitValues(test.in)
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestSplitValues(%s): -want/+got:\n%s", test.name, diff)
		}
	}
}

func TestGenerate(t *testing.T) {
	tests := []struct {
		name   string
		cfg    config
		sets   []setDef
		golden string // Name of the file in testdata/golden holding the source generate must produce.
	}{
		{
			name:   "Success: constants of one type",
			cfg:    config{pkg: "paint", args: []string{"-t", "Color"}},
			sets:   []setDef{{Type: "Color", Elems: "Blue, Red", Doc: "ColorSet is an immutable set holding the constant values of type Color."}},
			golden: "constants.golden",
		},
		{
			name:   "Success: explicit values",
			cfg:    config{pkg: "paint", args: []string{"-t", "Color", "-v", "blue,yellow"}},
			sets:   []setDef{{Type: "Color", Elems: `"blue", "yellow"`, Doc: "ColorSet is an immutable set of Color values."}},
			golden: "values.golden",
		},
		{
			name: "Success: multiple types in one file",
			cfg:  config{pkg: "paint", args: []string{"-t", "Color,ID"}},
			sets: []setDef{
				{Type: "Color", Elems: "Blue, Red", Doc: "ColorSet is an immutable set holding the constant values of type Color."},
				{Type: "ID", Elems: "One, Zero", Doc: "IDSet is an immutable set holding the constant values of type ID."},
			},
			golden: "multiTypes.golden",
		},
	}

	for _, test := range tests {
		got, err := generate(test.cfg, test.sets)
		if err != nil {
			t.Errorf("TestGenerate(%s): got err == %s, want err == nil", test.name, err)
			continue
		}

		path := filepath.Join("testdata", "golden", test.golden)
		if *update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				panic(err)
			}
			if err := os.WriteFile(path, got, 0o644); err != nil {
				panic(err)
			}
		}

		want, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("TestGenerate(%s): reading golden file %s: got err == %s, want err == nil; re-run with -update to create it", test.name, test.golden, err)
			continue
		}
		if diff := pretty.Compare(sourceLines(string(want)), sourceLines(string(got))); diff != "" {
			t.Errorf("TestGenerate(%s): generated output differs from golden file %s, -want/+got:\n%s\nIf the change is intended, re-run with -update", test.name, test.golden, diff)
		}
	}
}
