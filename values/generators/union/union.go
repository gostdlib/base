// Union is a tool to automate the creation of type-safe union (sum) types in Go.
// Given a union name and a set of member types, it generates a self-contained Go
// source file implementing a value type that can hold exactly one of the member
// types, along with a discriminator enum, typed setters, and typed accessors.
//
// For example, given these types in package candy:
//
//	type Twix struct{ String string }
//	type ThreeMuskateers struct{ String string }
//
// running this command in the same directory:
//
//	union -n Candy -t Twix,ThreeMuskateers
//
// generates candy_union.go containing:
//
//   - type CandyType uint8 with constants CandyTypeNotSet, CandyTypeTwix and
//     CandyTypeThreeMuskateers.
//   - type Candy struct holding the discriminator and the value. Its zero value
//     holds no member.
//   - func (c *Candy) Type() CandyType reporting which member is held.
//   - func (c *Candy) SetTwix(v Twix) Twix and func (c *Candy) SetThreeMuskateers(v ThreeMuskateers) ThreeMuskateers
//     replacing any current member with the given value and returning it.
//   - func (c *Candy) Twix() Twix and func (c *Candy) ThreeMuskateers() ThreeMuskateers
//     returning the held value or the zero value if a different member is held.
//
// A Candy is created from its zero value and populated with a Set method:
//
//	var c Candy
//	c.SetTwix(Twix{String: "hello"})
//
// Typically this process is run using go generate, like this:
//
//	//go:generate go tool github.com/gostdlib/base/generators/union -n Candy -t Twix,ThreeMuskateers
//
// The default output file is <name>_union.go, where <name> is the lower-cased
// union name. It can be overridden with the -output flag.
//
// By default the union stores its value in an any field. Passing -noAny instead
// gives the union one typed field per member, so a non-pointer member is stored
// without being boxed into an any (no heap allocation) and accessors return the
// field directly without a type assertion. The generated API is identical either
// way; -noAny only changes the internal representation.
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

var (
	name   = flag.String("n", "", "name of the union type to generate; must be set")
	types  = flag.String("t", "", "comma-separated list of member type names; must be set")
	output = flag.String("output", "", "output file name; default <name>_union.go")
	noAny  = flag.Bool("noAny", false, "store each member in its own typed field instead of an `any` field, avoiding boxing allocations for non-pointer members and type assertions on access")
)

// Usage is a replacement usage function for the flags package.
func Usage() {
	fmt.Fprintf(os.Stderr, "Usage of union:\n")
	fmt.Fprintf(os.Stderr, "\tunion -n Name -t Type1,Type2,... [flags]\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Union generates a type-safe union (sum) type holding one of the member types.\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("union: ")
	flag.Usage = Usage
	flag.Parse()

	cfg := config{name: *name, members: splitMembers(*types), output: *output, noAny: *noAny, args: os.Args[1:]}
	if err := cfg.validate(); err != nil {
		log.Print(err)
		flag.Usage()
		os.Exit(2)
	}

	pkg, err := packageName(".")
	if err != nil {
		log.Fatal(err)
	}
	cfg.pkg = pkg

	src, err := generate(cfg)
	if err != nil {
		log.Fatal(err)
	}

	out := cfg.output
	if out == "" {
		out = strings.ToLower(cfg.name) + "_union.go"
	}
	if err := os.WriteFile(out, src, 0o644); err != nil {
		log.Fatalf("writing output: %s", err)
	}
}

// config holds the parsed inputs for a single generation run.
type config struct {
	pkg     string   // package the generated file belongs to.
	name    string   // union type name, e.g. "Candy".
	members []string // member type names, e.g. ["Twix", "ThreeMuskateers"].
	output  string   // output file name override; empty means default.
	noAny   bool     // store each member in its own typed field instead of an any field.
	args    []string // the args passed to the command, for the generated header.
}

// validate reports whether the config holds usable inputs.
func (c config) validate() error {
	switch {
	case c.name == "":
		return fmt.Errorf("-n is required")
	case !isIdent(c.name):
		return fmt.Errorf("-n %q is not a valid Go identifier", c.name)
	case len(c.members) == 0:
		return fmt.Errorf("-t is required")
	}
	seen := map[string]bool{}
	for _, m := range c.members {
		switch {
		case !isIdent(m):
			return fmt.Errorf("-t member %q is not a valid Go identifier", m)
		case m == c.name:
			return fmt.Errorf("-t member %q cannot share the union name", m)
		case seen[m]:
			return fmt.Errorf("-t member %q is listed more than once", m)
		}
		seen[m] = true
	}
	return nil
}

// splitMembers splits a comma-separated type list into trimmed, non-empty names.
func splitMembers(s string) []string {
	var out []string
	for _, m := range strings.Split(s, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// member describes one union member for the template.
type member struct {
	Type  string // the member type name, e.g. "Twix".
	Const string // the discriminator constant, e.g. "CandyTypeTwix".
	Value int    // the discriminator value, e.g. 1.
	Field string // the -noAny storage field name, e.g. "vTwix".
}

// tmplData is the data passed to the output template.
type tmplData struct {
	Args     string   // the command-line args, for the header comment.
	Pkg      string   // the package name.
	Name     string   // the union type name.
	Recv     string   // the method receiver name.
	EnumType string   // the discriminator type name, e.g. "CandyType".
	List     string   // human-readable member list for doc comments.
	Members  []member // the union members.
	NoAny    bool     // store each member in its own typed field instead of an any field.

	// NameStr, Index and IndexBits implement stringer's name-string + index-array
	// lookup for the discriminator's String method. NameStr is the constant names
	// concatenated; Index holds the offset of each name's end (with a leading 0);
	// IndexBits is the width of the smallest uint that can hold the largest offset.
	NameStr   string // e.g. "CandyTypeNotSetCandyTypeTwix...".
	Index     string // e.g. "0, 15, 28, 52".
	IndexBits int    // 8, 16 or 32.
}

// generate produces the gofmt-ed source for the union described by c.
func generate(c config) ([]byte, error) {
	enum := c.name + "Type"

	members := make([]member, len(c.members))
	for i, m := range c.members {
		members[i] = member{Type: m, Const: enum + m, Value: i + 1, Field: "v" + m}
	}

	// Build stringer's name string and index array. The constants are 0..n and
	// contiguous (NotSet=0 followed by each member), so a single run suffices.
	names := []string{enum + "NotSet"}
	for _, m := range members {
		names = append(names, m.Const)
	}
	var b strings.Builder
	offsets := []string{"0"}
	for _, n := range names {
		b.WriteString(n)
		offsets = append(offsets, strconv.Itoa(b.Len()))
	}

	data := tmplData{
		Args:      strings.Join(c.args, " "),
		Pkg:       c.pkg,
		Name:      c.name,
		Recv:      receiver(c.name),
		EnumType:  enum,
		List:      strings.Join(c.members, ", "),
		Members:   members,
		NoAny:     c.noAny,
		NameStr:   b.String(),
		Index:     strings.Join(offsets, ", "),
		IndexBits: usize(b.Len()),
	}

	buf := &bytes.Buffer{}
	if err := unionTmpl.Execute(buf, data); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}

	src, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated source: %w\n%s", err, buf.Bytes())
	}
	return src, nil
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

// receiver returns the method receiver name for the union type.
func receiver(name string) string {
	return strings.ToLower(string([]rune(name)[0]))
}

// usize returns the number of bits of the smallest unsigned integer type that
// will hold n. It is used to size the index array of name offsets.
func usize(n int) int {
	switch {
	case n < 1<<8:
		return 8
	case n < 1<<16:
		return 16
	default:
		return 32
	}
}

var unionTmpl = template.Must(template.New("union").Parse(`// Code generated by "union {{.Args}}"; DO NOT EDIT.

package {{.Pkg}}

import "strconv"

// {{.EnumType}} is the discriminator for the {{.Name}} union. It reports which
// member type a {{.Name}} currently holds.
type {{.EnumType}} uint8

const (
	// {{.EnumType}}NotSet indicates the {{.Name}} holds no member; it is the zero value.
	{{.EnumType}}NotSet {{.EnumType}} = 0
{{- range .Members}}
	// {{.Const}} indicates the {{$.Name}} holds a {{.Type}}.
	{{.Const}} {{$.EnumType}} = {{.Value}}
{{- end}}
)

const _{{.EnumType}}_name = "{{.NameStr}}"

var _{{.EnumType}}_index = [...]uint{{.IndexBits}}{ {{.Index}} }

// String returns the name of the {{.EnumType}} constant, or, for an unknown
// value, "{{.EnumType}}(N)" where N is the underlying integer.
func (t {{.EnumType}}) String() string {
	if int(t) >= len(_{{.EnumType}}_index)-1 {
		return "{{.EnumType}}(" + strconv.FormatUint(uint64(t), 10) + ")"
	}
	return _{{.EnumType}}_name[_{{.EnumType}}_index[t]:_{{.EnumType}}_index[t+1]]
}

// {{.Name}} is a union type that holds exactly one of the following member
// types: {{.List}}. The zero value holds no member; use a Set method to set one.
type {{.Name}} struct {
	t {{.EnumType}}
{{- if .NoAny}}
{{- range .Members}}
	{{.Field}} {{.Type}}
{{- end}}
{{- else}}
	v any
{{- end}}
}

// Type reports which member type {{.Recv}} currently holds.
func ({{.Recv}} *{{.Name}}) Type() {{.EnumType}} {
	return {{.Recv}}.t
}
{{range .Members}}
// Set{{.Type}} unsets {{$.Recv}}'s current member, if any, sets it to a {{.Type}}
// holding v, and returns v.
func ({{$.Recv}} *{{$.Name}}) Set{{.Type}}(v {{.Type}}) {{.Type}} {
	*{{$.Recv}} = {{$.Name}}{}
	{{$.Recv}}.t = {{.Const}}
{{- if $.NoAny}}
	{{$.Recv}}.{{.Field}} = v
{{- else}}
	{{$.Recv}}.v = v
{{- end}}
	return v
}

// {{.Type}} returns the {{.Type}} held by {{$.Recv}}. If {{$.Recv}} does not hold a
// {{.Type}}, the zero value is returned; compare {{$.Recv}}.Type() to {{.Const}} to
// distinguish a held zero value from an absent one.
func ({{$.Recv}} *{{$.Name}}) {{.Type}}() {{.Type}} {
{{- if $.NoAny}}
	return {{$.Recv}}.{{.Field}}
{{- else}}
	if {{$.Recv}}.t != {{.Const}} {
		var v {{.Type}}
		return v
	}
	return {{$.Recv}}.v.({{.Type}})
{{- end}}
}
{{end}}`))
