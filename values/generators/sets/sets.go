// Sets is a tool to automate the creation of immutable set values in Go. Given the name of a
// type T declared in the package that has constants defined, sets will create a new
// self-contained Go source file declaring
//
//	var TSet = immutable.NewSet([]T{ ...the constants of type T... })
//
// where immutable.Set is github.com/gostdlib/base/values/immutable.Set. The file is created in
// the same package and directory as the package that defines T. It has helpful defaults
// designed for use with go generate.
//
// For example, given this snippet,
//
//	package paint
//
//	type Color string
//
//	const (
//		Blue Color = "blue"
//		Red  Color = "yellow"
//	)
//
// running this command
//
//	sets -t Color
//
// in the same directory will create the file color_set.go, in package paint, containing
//
//	var ColorSet = immutable.NewSet([]Color{Blue, Red})
//
// Typically this process would be run using go generate, like this:
//
//	//go:generate go tool github.com/gostdlib/base/values/generators/sets -t Color
//
// Instead of collecting the package's constants, the values can be given explicitly with the
// -v flag, which requires exactly one type:
//
//	sets -t Color -v blue,yellow
//
// generates
//
//	var ColorSet = immutable.NewSet([]Color{"blue", "yellow"})
//
// In both modes the type's underlying type must be a string, int/int8/int16/int32/int64,
// uint/uint8/uint16/uint32/uint64, or float32/float64; -v values are validated against that
// underlying type.
//
// With no arguments, it processes the package in the current directory. Otherwise, the one
// argument must name a directory holding a Go package.
//
// The -t flag accepts a comma-separated list of types so a single run can generate sets
// for multiple types. The default output file is t_set.go, where t is the lower-cased name of
// the first type listed. It can be overridden with the -output flag.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"go/types"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"unicode"

	"golang.org/x/tools/go/packages"
)

var (
	typeNames = flag.String("t", "", "comma-separated list of type names; must be set")
	values    = flag.String("v", "", "comma-separated list of values instead of the package's constants; requires exactly one -t type")
	output    = flag.String("output", "", "output file name; default srcdir/<type>_set.go")
)

// Usage is a replacement usage function for the flags package.
func Usage() {
	fmt.Fprintf(os.Stderr, "Usage of sets:\n")
	fmt.Fprintf(os.Stderr, "\tsets [flags] -t T [directory]\n")
	fmt.Fprintf(os.Stderr, "\tsets [flags] -t T -v value,value,... [directory]\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Sets generates a var TSet = immutable.NewSet(...) holding the constants of type T,\n")
	fmt.Fprintf(os.Stderr, "or the values given with -v.\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("sets: ")
	flag.Usage = Usage
	flag.Parse()

	cfg := config{types: splitList(*typeNames), values: splitList(*values), output: *output, args: os.Args[1:]}
	if err := cfg.validate(); err != nil {
		log.Print(err)
		flag.Usage()
		os.Exit(2)
	}

	dir := "."
	if args := flag.Args(); len(args) > 0 {
		dir = args[0]
	}

	pkg, sets, err := analyze(dir, cfg)
	if err != nil {
		log.Fatal(err)
	}
	cfg.pkg = pkg

	src, err := generate(cfg, sets)
	if err != nil {
		log.Fatal(err)
	}

	out := cfg.output
	if out == "" {
		out = strings.ToLower(cfg.types[0]) + "_set.go"
	}
	if err := os.WriteFile(filepath.Join(dir, out), src, 0o644); err != nil {
		log.Fatalf("writing output: %s", err)
	}
}

// config holds the parsed inputs for a single generation run.
type config struct {
	pkg    string   // package the generated file belongs to.
	types  []string // names of the types to generate sets for, e.g. ["Color"].
	values []string // explicit values from -v; empty means collect the package's constants.
	output string   // output file name override; empty means default.
	args   []string // the args passed to the command, for the generated header.
}

// validate reports whether the config holds usable inputs.
func (c config) validate() error {
	if len(c.types) == 0 {
		return fmt.Errorf("-t is required")
	}
	if len(c.values) > 0 && len(c.types) != 1 {
		return fmt.Errorf("-v requires exactly one -t type, got %d", len(c.types))
	}
	seen := map[string]bool{}
	for _, t := range c.types {
		switch {
		case !isIdent(t):
			return fmt.Errorf("-t %q is not a valid Go identifier", t)
		case seen[t]:
			return fmt.Errorf("-t %q is listed more than once", t)
		}
		seen[t] = true
	}
	return nil
}

// splitList splits a comma-separated list into trimmed, non-empty entries.
func splitList(s string) []string {
	var out []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// setDef describes one generated set var for the template.
type setDef struct {
	Type  string // the element type name, e.g. "Color".
	Elems string // the set elements as Go expressions, e.g. `Blue, Red` or `"blue", "yellow"`.
	Doc   string // the var's doc comment text.
}

// analyze loads the package in dir and builds a setDef for each type in c. Each type must be
// declared in the package with an underlying string, integer, or float type. Without -v values,
// the set's elements are the package's constants of that type; with them, the values are
// validated against the underlying type and emitted as literals. It returns the package's name.
func analyze(dir string, c config) (string, []setDef, error) {
	// NeedSyntax and NeedTypesInfo force the package to be type-checked from source. Without them
	// the loader may satisfy NeedTypes from compiled export data, which holds only exported names,
	// silently hiding unexported types and dropping unexported constants from the set.
	mode := packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedFiles
	cfg := &packages.Config{Mode: mode, Dir: dir}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return "", nil, fmt.Errorf("loading package in %q: %w", dir, err)
	}
	if len(pkgs) != 1 {
		return "", nil, fmt.Errorf("expected one package in %q, got %d", dir, len(pkgs))
	}
	pkg := pkgs[0]
	if pkg.Types == nil {
		return "", nil, fmt.Errorf("could not type-check the package in %q%s", dir, pkgErrs(pkg))
	}
	// Type errors are tolerated. A package that uses the set does not compile until this tool has
	// generated it, which is exactly the state it is in when go:generate first runs. Errors are
	// only reported below, where they explain why a type could not be resolved.

	sets := make([]setDef, 0, len(c.types))
	for _, name := range c.types {
		obj := pkg.Types.Scope().Lookup(name)
		if obj == nil {
			return "", nil, fmt.Errorf("type %q is not declared in package %s%s", name, pkg.Name, pkgErrs(pkg))
		}
		tn, ok := obj.(*types.TypeName)
		if !ok {
			return "", nil, fmt.Errorf("%q is not a type in package %s", name, pkg.Name)
		}
		basic, ok := tn.Type().Underlying().(*types.Basic)
		if !ok || kindBits(basic.Kind()) == 0 {
			return "", nil, fmt.Errorf("type %q must have an underlying string, integer, or float type, have %s", name, tn.Type().Underlying())
		}

		var elems []string
		if len(c.values) > 0 {
			for _, v := range c.values {
				lit, err := literal(basic.Kind(), v)
				if err != nil {
					return "", nil, fmt.Errorf("-v value %q is not a valid %s: %w", v, name, err)
				}
				elems = append(elems, lit)
			}
			sets = append(sets, setDef{Type: name, Elems: strings.Join(elems, ", "), Doc: fmt.Sprintf("%sSet is an immutable set of %s values.", name, name)})
			continue
		}

		// Scope names are sorted, so the constants come out in a deterministic order.
		for _, n := range pkg.Types.Scope().Names() {
			if c, ok := pkg.Types.Scope().Lookup(n).(*types.Const); ok && types.Identical(c.Type(), tn.Type()) {
				elems = append(elems, n)
			}
		}
		if len(elems) == 0 {
			return "", nil, fmt.Errorf("type %q has no constants declared in package %s", name, pkg.Name)
		}
		sets = append(sets, setDef{Type: name, Elems: strings.Join(elems, ", "), Doc: fmt.Sprintf("%sSet is an immutable set holding the constant values of type %s.", name, name)})
	}
	return pkg.Name, sets, nil
}

// pkgErrs renders a package's load and type errors as a trailing clause for an error message,
// or "" when there are none. A type that cannot be resolved is usually explained by them.
func pkgErrs(pkg *packages.Package) string {
	if len(pkg.Errors) == 0 {
		return ""
	}
	msgs := make([]string, 0, len(pkg.Errors))
	for _, e := range pkg.Errors {
		msgs = append(msgs, e.Error())
	}
	return "; the package has errors that may explain this: " + strings.Join(msgs, "; ")
}

// kindBits returns the bit size used to validate values of the given basic kind, or 0 if the
// kind is not allowed as a set element.
func kindBits(k types.BasicKind) int {
	switch k {
	case types.String:
		return 1 // Unused; any string is valid.
	case types.Int, types.Uint:
		return strconv.IntSize
	case types.Int8, types.Uint8:
		return 8
	case types.Int16, types.Uint16:
		return 16
	case types.Int32, types.Uint32, types.Float32:
		return 32
	case types.Int64, types.Uint64, types.Float64:
		return 64
	}
	return 0
}

// literal validates v against the basic kind and returns it as a Go literal.
func literal(k types.BasicKind, v string) (string, error) {
	bits := kindBits(k)
	switch k {
	case types.String:
		return strconv.Quote(v), nil
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64:
		if _, err := strconv.ParseInt(v, 0, bits); err != nil {
			return "", err
		}
	case types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
		if _, err := strconv.ParseUint(v, 0, bits); err != nil {
			return "", err
		}
	case types.Float32, types.Float64:
		if _, err := strconv.ParseFloat(v, bits); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported kind")
	}
	return v, nil
}

// tmplData is the data passed to the output template.
type tmplData struct {
	Args string   // the command-line args, for the header comment.
	Pkg  string   // the package name.
	Sets []setDef // the sets to generate.
}

// generate produces the gofmt-ed source for the sets.
func generate(c config, sets []setDef) ([]byte, error) {
	data := tmplData{
		Args: strings.Join(c.args, " "),
		Pkg:  c.pkg,
		Sets: sets,
	}

	buf := &bytes.Buffer{}
	if err := setTmpl.Execute(buf, data); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}

	src, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated source: %w\n%s", err, buf.Bytes())
	}
	return src, nil
}

// isIdent reports whether s is a valid Go identifier.
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case unicode.IsLetter(r) || r == '_':
		case unicode.IsDigit(r) && i > 0:
		default:
			return false
		}
	}
	return true
}

var setTmpl = template.Must(template.New("sets").Parse(`// Code generated by "sets {{.Args}}"; DO NOT EDIT.

package {{.Pkg}}

import "github.com/gostdlib/base/values/immutable"
{{range .Sets}}
// {{.Doc}}
var {{.Type}}Set = immutable.NewSet([]{{.Type}}{ {{.Elems}} })
{{end}}`))
