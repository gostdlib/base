// Tuple is a tool to automate the creation of small, named tuple types in Go. Given a type name and a list of field
// types, it generates a value type holding those fields along with a constructor, positional or named accessors, a Len
// method and a String method.
//
// For example, running this command in package person:
//
//	tuple -p lastFirst string, string
//
// generates lastfirst.go containing:
//
//	type lastFirst struct {
//		v0 string
//		v1 string
//	}
//
//	func NewlastFirst(v0 string, v1 string) lastFirst { ... }
//	func (t lastFirst) V0() string { ... }
//	func (t lastFirst) V1() string { ... }
//	func (t lastFirst) Len() int { ... }
//	func (t lastFirst) String() string { ... }
//
// Fields may be named by prefixing the type with "name:". Named fields replace the positional accessor (V0, V1, ...)
// with an exported accessor derived from the name. For example:
//
//	tuple -p lastFirst last:string, first:string
//
// generates a lastFirst tuple whose accessors are Last() and First() instead of V0() and V1().
//
// The type name is used verbatim, so its exported/unexported case is whatever is passed on the command line:
// "lastFirst" generates an unexported type, while "LastFirst" generates an exported one.
//
// The -p flag controls output. With -p, a complete Go source file (package clause and imports) is written into the
// current directory as <name>.go, lower-cased. Without -p, only the type and its methods are printed to standard
// output, with no package clause or imports, for pasting into an existing file.
//
// Typically this process is run using go generate, like this:
//
//	//go:generate go tool github.com/gostdlib/base/values/generators/tuple -p lastFirst string, string
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"unicode"
)

var preamble = flag.Bool("p", false, "write a complete Go file (package clause and imports) into the current package as <name>.go; without it, only the type and methods are printed to stdout")

// Usage is a replacement usage function for the flags package.
func Usage() {
	fmt.Fprintf(os.Stderr, "Usage of tuple:\n")
	fmt.Fprintf(os.Stderr, "\ttuple [-p] Name type[, type...]\n")
	fmt.Fprintf(os.Stderr, "\ttuple [-p] Name name:type[, name:type...]\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Tuple generates a small value type holding the given fields, with a\n")
	fmt.Fprintf(os.Stderr, "constructor, accessors, a Len method and a String method.\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("tuple: ")
	flag.Usage = Usage
	flag.Parse()

	cfg := config{preamble: *preamble, args: argsForHeader()}
	if name, fields, err := parseArgs(flag.Args()); err != nil {
		log.Print(err)
		flag.Usage()
		os.Exit(2)
	} else {
		cfg.name, cfg.fields = name, fields
	}
	if err := cfg.validate(); err != nil {
		log.Print(err)
		flag.Usage()
		os.Exit(2)
	}

	if cfg.preamble {
		pkg, err := packageName(".")
		if err != nil {
			log.Fatal(err)
		}
		cfg.pkg = pkg
	}

	src, err := generate(cfg)
	if err != nil {
		log.Fatal(err)
	}

	if !cfg.preamble {
		if _, err := os.Stdout.Write(src); err != nil {
			log.Fatalf("writing output: %s", err)
		}
		return
	}

	out := strings.ToLower(cfg.name) + ".go"
	if err := os.WriteFile(out, src, 0o644); err != nil {
		log.Fatalf("writing output: %s", err)
	}
}

// field describes one tuple field.
type field struct {
	Field    string // the struct field name, e.g. "v0" or "last".
	Accessor string // the exported accessor method name, e.g. "V0" or "Last".
	Type     string // the field's Go type, e.g. "string".
	Ordinal  string // the field's position in words, e.g. "first".
}

// config holds the parsed inputs for a single generation run.
type config struct {
	pkg      string  // package the generated file belongs to; only used with preamble.
	name     string  // tuple type name, used verbatim, e.g. "lastFirst".
	fields   []field // the tuple fields, in order.
	preamble bool    // emit a full file (package clause and imports) instead of a fragment.
	args     []string
}

// validate reports whether the config holds usable inputs.
func (c config) validate() error {
	switch {
	case c.name == "":
		return fmt.Errorf("a tuple name is required")
	case !isIdent(c.name):
		return fmt.Errorf("tuple name %q is not a valid Go identifier", c.name)
	case len(c.fields) == 0:
		return fmt.Errorf("at least one field type is required")
	}
	seen := map[string]bool{}
	for _, f := range c.fields {
		switch {
		case f.Type == "":
			return fmt.Errorf("field %q has an empty type", f.Field)
		case seen[f.Field]:
			return fmt.Errorf("field name %q is listed more than once", f.Field)
		}
		seen[f.Field] = true
	}
	return nil
}

// parseArgs splits the positional command arguments into the tuple name and its fields. The first argument is the name;
// the remainder is a comma-separated list of "type" or "name:type" field specifications.
func parseArgs(args []string) (string, []field, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("a tuple name is required")
	}
	name := args[0]
	var fields []field
	for i, spec := range splitFields(strings.Join(args[1:], " ")) {
		f := field{Type: spec, Ordinal: ordinal(i)}
		if n, t, ok := cutFieldSpec(spec); ok {
			n, t = strings.TrimSpace(n), strings.TrimSpace(t)
			if !isIdent(n) {
				return "", nil, fmt.Errorf("field name %q is not a valid Go identifier", n)
			}
			f.Field = unexport(n)
			f.Accessor = export(n)
			f.Type = t
		} else {
			f.Field = "v" + strconv.Itoa(i)
			f.Accessor = "V" + strconv.Itoa(i)
		}
		fields = append(fields, f)
	}
	return name, fields, nil
}

// splitFields splits a comma-separated field list into trimmed, non-empty specs. Only commas at bracket depth zero and
// outside of string, rune, or raw string literals separate fields, so commas inside a field's type (function parameter
// lists, generic type arguments, struct types with tags, and the like) are preserved. For example
// "func(int, string), string" splits into "func(int, string)" and "string".
func splitFields(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\'', '`':
			i = skipLiteral(s, i) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			// Clamp at zero so a stray or mismatched closer can't drive depth negative and cause a
			// top-level comma to be treated as nested (silently merging two fields).
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				if f := strings.TrimSpace(s[start:i]); f != "" {
					out = append(out, f)
				}
				start = i + 1
			}
		}
	}
	if f := strings.TrimSpace(s[start:]); f != "" {
		out = append(out, f)
	}
	return out
}

// cutFieldSpec splits a field spec into an optional name and its type. A field spec is either "type" or "name:type";
// only a colon at bracket depth zero and outside of string, rune, or raw string literals separates the name from the
// type, so colons inside a type (most notably struct tags like `json:"x"`) are not mistaken for the separator. It
// reports whether a name was present.
func cutFieldSpec(spec string) (name, typ string, named bool) {
	depth := 0
	for i := 0; i < len(spec); i++ {
		switch spec[i] {
		case '"', '\'', '`':
			i = skipLiteral(spec, i) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				return spec[:i], spec[i+1:], true
			}
		}
	}
	return spec, "", false
}

// skipLiteral, given the index i of an opening quote (", ', or `) in s, returns the index just past the matching
// closing quote. Interpreted string and rune literals honor backslash escapes; raw string literals (backtick) do not.
// If no closer is found, it returns len(s).
func skipLiteral(s string, i int) int {
	quote := s[i]
	raw := quote == '`'
	for j := i + 1; j < len(s); j++ {
		switch {
		case !raw && s[j] == '\\':
			j++ // skip the escaped byte.
		case s[j] == quote:
			return j + 1
		}
	}
	return len(s)
}

// tmplData is the data passed to the output template.
type tmplData struct {
	Args     string  // the command-line args, for the header comment.
	Preamble bool    // emit the package clause and imports.
	Pkg      string  // the package name.
	Name     string  // the tuple type name.
	List     string  // human-readable field list for the type doc comment.
	Fields   []field // the tuple fields.
	N        int     // the number of fields.
	Params   string  // the constructor parameter list, e.g. "v0 string, v1 string".
	Inits    string  // the struct literal field assignments, e.g. "v0: v0, v1: v1".
	Format   string  // the String format verb list, e.g. "(%v, %v)".
	StrArgs  string  // the String arguments, e.g. "t.v0, t.v1".
}

// generate produces the gofmt-ed source for the tuple described by c.
func generate(c config) ([]byte, error) {
	params := make([]string, len(c.fields))
	inits := make([]string, len(c.fields))
	strArgs := make([]string, len(c.fields))
	list := make([]string, len(c.fields))
	for i, f := range c.fields {
		params[i] = f.Field + " " + f.Type
		inits[i] = f.Field + ": " + f.Field
		strArgs[i] = "t." + f.Field
		list[i] = f.Field + " " + f.Type
	}

	data := tmplData{
		Args:     strings.Join(c.args, " "),
		Preamble: c.preamble,
		Pkg:      c.pkg,
		Name:     c.name,
		List:     strings.Join(list, ", "),
		Fields:   c.fields,
		N:        len(c.fields),
		Params:   strings.Join(params, ", "),
		Inits:    strings.Join(inits, ", "),
		Format:   "(" + strings.TrimSuffix(strings.Repeat("%v, ", len(c.fields)), ", ") + ")",
		StrArgs:  strings.Join(strArgs, ", "),
	}

	buf := &bytes.Buffer{}
	if err := tupleTmpl.Execute(buf, data); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}

	src, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated source: %w\n%s", err, buf.Bytes())
	}
	return src, nil
}

// argsForHeader returns the original command arguments, dropping the leading
// program name, for the generated header comment.
func argsForHeader() []string {
	if len(os.Args) <= 1 {
		return nil
	}
	return os.Args[1:]
}

// packageName returns the package name declared by the Go files in dir. It
// prefers the GOPACKAGE environment variable set by "go generate" and falls
// back to parsing the directory's package clauses.
func packageName(dir string) (string, error) {
	if p := os.Getenv("GOPACKAGE"); p != "" {
		return p, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("reading directory %q: %w", dir, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.PackageClauseOnly)
		if err != nil {
			continue
		}
		if p := f.Name.Name; p != "" && !strings.HasSuffix(p, "_test") {
			return p, nil
		}
	}
	return "", fmt.Errorf("could not determine package name in %q; set GOPACKAGE or run via go generate", dir)
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

// export returns s with its first rune upper-cased.
func export(s string) string {
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// unexport returns s with its first rune lower-cased.
func unexport(s string) string {
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// ordinal returns the English word for the (i+1)-th position, e.g. ordinal(0)
// is "first". Beyond the tenth position it falls back to "(i+1)-th".
func ordinal(i int) string {
	words := []string{"first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth", "ninth", "tenth"}
	if i < len(words) {
		return words[i]
	}
	return strconv.Itoa(i+1) + "-th"
}

var tupleTmpl = template.Must(template.New("tuple").Parse(`{{if .Preamble}}// Code generated by "tuple {{.Args}}"; DO NOT EDIT.

package {{.Pkg}}

import "fmt"

{{end}}// {{.Name}} is a tuple holding {{.N}} values: {{.List}}.
type {{.Name}} struct {
{{- range .Fields}}
	{{.Field}} {{.Type}}
{{- end}}
}

// New{{.Name}} creates a new {{.Name}} tuple with the given values.
func New{{.Name}}({{.Params}}) {{.Name}} {
	return {{.Name}}{ {{.Inits}} }
}
{{range .Fields}}
// {{.Accessor}} returns the {{.Ordinal}} value of the tuple.
func (t {{$.Name}}) {{.Accessor}}() {{.Type}} {
	return t.{{.Field}}
}
{{end}}
// Len returns the number of values in the tuple.
func (t {{.Name}}) Len() int {
	return {{.N}}
}

// String implements fmt.Stringer.
func (t {{.Name}}) String() string {
	return fmt.Sprintf("{{.Format}}", {{.StrArgs}})
}
`))
