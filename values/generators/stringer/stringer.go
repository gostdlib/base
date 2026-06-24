// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Stringer is a tool to automate the creation of methods that satisfy the fmt.Stringer
// interface. Given the name of a (signed or unsigned) integer type T that has constants
// defined, stringer will create a new self-contained Go source file implementing
//
//	func (t T) String() string
//
// The file is created in the same package and directory as the package that defines T.
// It has helpful defaults designed for use with go generate.
//
// Stringer works best with constants that are consecutive values such as created using iota,
// but creates good code regardless. In the future it might also provide custom support for
// constant sets that are bit patterns.
//
// For example, given this snippet,
//
//	package painkiller
//
//	type Pill int
//
//	const (
//		Placebo Pill = iota
//		Aspirin
//		Ibuprofen
//		Paracetamol
//		Acetaminophen = Paracetamol
//	)
//
// running this command
//
//	stringer -type=Pill
//
// in the same directory will create the file pill_string.go, in package painkiller,
// containing a definition of
//
//	func (Pill) String() string
//
// That method will translate the value of a Pill constant to the string representation
// of the respective constant name, so that the call fmt.Print(painkiller.Aspirin) will
// print the string "Aspirin".
//
// Typically this process would be run using go generate, like this:
//
//	//go:generate stringer -type=Pill
//
// If multiple constants have the same value, the lexically first matching name will
// be used (in the example, Acetaminophen will print as "Paracetamol").
//
// With no arguments, it processes the package in the current directory.
// Otherwise, the arguments must name a single directory holding a Go package
// or a set of Go source files that represent a single Go package.
//
// The -type flag accepts a comma-separated list of types so a single run can
// generate methods for multiple types. The default output file is t_string.go,
// where t is the lower-cased name of the first type listed. It can be overridden
// with the -output flag.
//
// Types can also be declared in tests, in which case type declarations in the
// non-test package or its test variant are preferred over types defined in the
// package with suffix "_test".
// The default output file for type declarations in tests is t_string_test.go with t picked as above.
//
// The -linecomment flag tells stringer to generate the text of any line comment, trimmed
// of leading spaces, instead of the constant name. For instance, if the constants above had a
// Pill prefix, one could write
//
//	PillAspirin // Aspirin
//
// to suppress it in the output.
//
// The -trimprefix flag specifies a prefix to remove from the constant names
// when generating the string representations. For instance, -trimprefix=Pill
// would be an alternative way to ensure that PillAspirin.String() == "Aspirin".
//
// # Additional Methods
//
// The -valid flag generates a Valid() bool method that returns true if the value
// is one of the defined constants.
//
// The -invalid flag accepts a comma-separated list of values or ranges that should
// be considered invalid. For example, -invalid="0,<4,>=100" marks 0, values less than 4,
// and values greater than or equal to 100 as invalid. This affects the Valid() method
// and the reverse lookup function.
//
// The -reverse flag generates a Reverse{{Type}}(s string, caseSensitive bool) ({{Type}}, bool)
// function that performs a reverse lookup from string to enum value.
//
// The -replace flag (can be used multiple times) specifies string replacements to apply
// in the reverse lookup function. For example, -replace=-,_ will replace dashes with
// underscores before lookup. Requires -reverse.
//
// The flag supports backslash escaping for special characters:
//   \,  → literal comma (allows replacing commas)
//   \\  → literal backslash
// For example, -replace=\,,_ replaces commas with underscores.
//
// When multiple -replace flags are used, they are applied sequentially in the order
// specified. For example, -replace=a,b -replace=b,c will transform "a" to "c" (a→b→c).
//
// The -marshal flag generates MarshalJSON and UnmarshalJSON methods for JSON encoding/decoding.
// The methods use the String() representation for marshaling and the Reverse{{Type}} function
// for unmarshaling. MarshalJSON validates the value using Valid() before marshaling, returning
// an error for invalid values. Requires -reverse and automatically enables -valid.
//
// The -marshalinsensitive flag makes UnmarshalJSON case-insensitive when looking up string values.
// For example, "red", "Red", and "RED" would all unmarshal to the same value. Automatically enables
// -marshal (and transitively -reverse and -valid).
//
// The -list flag generates a List{{Type}}() iter.Seq[{{Type}}] function that yields all defined
// constant values in order.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/token"
	"go/types"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// replaceFlags implements flag.Value to support multiple --replace flags.
type replaceFlags []string

func (r *replaceFlags) String() string {
	return strings.Join(*r, ",")
}

func (r *replaceFlags) Set(value string) error {
	*r = append(*r, value)
	return nil
}

var (
	typeNames   = flag.String("type", "", "comma-separated list of type names; must be set")
	output      = flag.String("output", "", "output file name; default srcdir/<type>_string.go")
	trimprefix  = flag.String("trimprefix", "", "trim the `prefix` from the generated constant names")
	linecomment = flag.Bool("linecomment", false, "use line comment text as printed text when present")
	buildTags   = flag.String("tags", "", "comma-separated list of build tags to apply")
	valid       = flag.Bool("valid", false, "generate Valid() bool method to check if value is defined")
	invalid     = flag.String("invalid", "", "comma-separated list of invalid values/ranges (e.g., \"0,-1,<4,>=100\")")
	reverse            = flag.Bool("reverse", false, "generate Reverse{{Type}} function for string to value lookup")
	marshal            = flag.Bool("marshal", false, "generate MarshalJSON/UnmarshalJSON methods (requires -reverse)")
	marshalinsensitive = flag.Bool("marshalinsensitive", false, "make UnmarshalJSON case-insensitive (automatically enables -marshal)")
	list               = flag.Bool("list", false, "generate List{{Type}}() iter.Seq[{{Type}}] function that yields all values in order")
	replace            replaceFlags
)

func init() {
	flag.Var(&replace, "replace", "comma-separated old,new pair for string replacement in Reverse (requires -reverse, can be used multiple times)")
}

// Usage is a replacement usage function for the flags package.
func Usage() {
	fmt.Fprintf(os.Stderr, "Usage of stringer:\n")
	fmt.Fprintf(os.Stderr, "\tstringer [flags] -type T [directory]\n")
	fmt.Fprintf(os.Stderr, "\tstringer [flags] -type T files... # Must be a single package\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Stringer generates String() methods for integer-based enum types.\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Additional methods can be generated:\n")
	fmt.Fprintf(os.Stderr, "  -valid: Generate Valid() bool method\n")
	fmt.Fprintf(os.Stderr, "  -invalid: Specify invalid values/ranges (e.g., \"0,<4,>=100\")\n")
	fmt.Fprintf(os.Stderr, "  -reverse: Generate Reverse{{Type}} function for string-to-value lookup\n")
	fmt.Fprintf(os.Stderr, "  -replace: Apply string replacements in reverse lookup (requires -reverse)\n")
	fmt.Fprintf(os.Stderr, "  -marshal: Generate JSON marshal methods (requires -reverse, enables -valid)\n")
	fmt.Fprintf(os.Stderr, "  -marshalinsensitive: Make UnmarshalJSON case-insensitive (enables -marshal)\n")
	fmt.Fprintf(os.Stderr, "  -list: Generate List{{Type}}() iter.Seq[{{Type}}] function\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "For more information, see:\n")
	fmt.Fprintf(os.Stderr, "\thttps://pkg.go.dev/golang.org/x/tools/cmd/stringer\n")
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("stringer: ")
	flag.Usage = Usage
	flag.Parse()
	if len(*typeNames) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	// Validate flag dependencies
	// marshalinsensitive automatically enables marshal (and marshal requires reverse)
	if *marshalinsensitive {
		*marshal = true
		*reverse = true
	}
	if *marshal && !*reverse {
		log.Fatal("-marshal flag requires -reverse flag")
	}
	// Marshal requires Valid() to check values before marshaling
	if *marshal {
		*valid = true
	}
	if len(replace) > 0 && !*reverse {
		log.Fatal("-replace flag requires -reverse flag")
	}

	types := strings.Split(*typeNames, ",")
	var tags []string
	if len(*buildTags) > 0 {
		tags = strings.Split(*buildTags, ",")
	}

	// We accept either one directory or a list of files. Which do we have?
	args := flag.Args()
	if len(args) == 0 {
		// Default: process whole package in current directory.
		args = []string{"."}
	}

	// Parse the package once.
	var dir string
	// TODO(suzmue): accept other patterns for packages (directories, list of files, import paths, etc).
	if len(args) == 1 && isDirectory(args[0]) {
		dir = args[0]
	} else {
		if len(tags) != 0 {
			log.Fatal("-tags option applies only to directories, not when files are specified")
		}
		dir = filepath.Dir(args[0])
	}

	// For each type, generate code in the first package where the type is declared.
	// The order of packages is as follows:
	// package x
	// package x compiled for tests
	// package x_test
	//
	// Each package pass could result in a separate generated file.
	// These files must have the same package and test/not-test nature as the types
	// from which they were generated.
	//
	// Types will be excluded when generated, to avoid repetitions.
	pkgs := loadPackages(args, tags, *trimprefix, *linecomment, nil /* logf */)
	sort.Slice(pkgs, func(i, j int) bool {
		// Put x_test packages last.
		iTest := strings.HasSuffix(pkgs[i].name, "_test")
		jTest := strings.HasSuffix(pkgs[j].name, "_test")
		if iTest != jTest {
			return !iTest
		}

		return len(pkgs[i].files) < len(pkgs[j].files)
	})
	for _, pkg := range pkgs {
		g := Generator{
			pkg:                pkg,
			valid:              *valid,
			invalidFlag:        *invalid,
			reverse:            *reverse,
			marshal:            *marshal,
			marshalinsensitive: *marshalinsensitive,
			list:               *list,
		}

		// Parse replacements if provided
		if len(replace) > 0 {
			if err := g.parseReplacements(replace); err != nil {
				log.Fatalf("parsing replacements: %v", err)
			}
		}

		// Print the header and package clause.
		g.Printf("// Code generated by \"stringer %s\"; DO NOT EDIT.\n", strings.Join(os.Args[1:], " "))
		g.Printf("\n")
		g.Printf("package %s", g.pkg.name)
		g.Printf("\n")
		if g.reverse || g.marshal || g.list {
			g.Printf("import (\n")
			if g.marshal {
				g.Printf("\t\"fmt\"\n")
			}
			if g.list {
				g.Printf("\t\"iter\"\n")
			}
			g.Printf("\t\"strconv\"\n")
			if g.reverse {
				g.Printf("\t\"strings\"\n")
			}
			if g.marshal {
				g.Printf("\t\"unsafe\"\n")
			}
			g.Printf(")\n")
		} else {
			g.Printf("import \"strconv\"\n") // Used by all methods.
		}

		// Run generate for types that can be found. Keep the rest for the remainingTypes iteration.
		var foundTypes, remainingTypes []string
		for _, typeName := range types {
			values := findValues(typeName, pkg)
			if len(values) > 0 {
				g.generate(typeName, values)
				foundTypes = append(foundTypes, typeName)
			} else {
				remainingTypes = append(remainingTypes, typeName)
			}
		}
		if len(foundTypes) == 0 {
			// This package didn't have any of the relevant types, skip writing a file.
			continue
		}
		if len(remainingTypes) > 0 && output != nil && *output != "" {
			log.Fatalf("cannot write to single file (-output=%q) when matching types are found in multiple packages", *output)
		}
		types = remainingTypes

		// Format the output.
		src := g.format()

		// Write to file.
		outputName := *output
		if outputName == "" {
			// Type names will be unique across packages since only the first
			// match is picked.
			// So there won't be collisions between a package compiled for tests
			// and the separate package of tests (package foo_test).
			outputName = filepath.Join(dir, baseName(pkg, foundTypes[0]))
		}
		err := os.WriteFile(outputName, src, 0o644)
		if err != nil {
			log.Fatalf("writing output: %s", err)
		}
	}

	if len(types) > 0 {
		log.Fatalf("no values defined for types: %s", strings.Join(types, ","))
	}
}

// baseName that will put the generated code together with pkg.
func baseName(pkg *Package, typename string) string {
	suffix := "string.go"
	if pkg.hasTestFiles {
		suffix = "string_test.go"
	}
	return fmt.Sprintf("%s_%s", strings.ToLower(typename), suffix)
}

// isDirectory reports whether the named file is a directory.
func isDirectory(name string) bool {
	info, err := os.Stat(name)
	if err != nil {
		log.Fatal(err)
	}
	return info.IsDir()
}

// InvalidRange represents a range or value that should be considered invalid.
type InvalidRange struct {
	op    string // "", "<", ">", "<=", ">="
	value int64  // the value to compare against
}

// ReplacePair represents an old,new string replacement pair.
type ReplacePair struct {
	old string
	new string
}

// Generator holds the state of the analysis. Primarily used to buffer
// the output for format.Source.
type Generator struct {
	buf           bytes.Buffer   // Accumulated output.
	pkg           *Package       // Package we are scanning.
	valid         bool           // Whether to generate Valid() method.
	invalidFlag   string         // The -invalid flag value.
	invalidRanges []InvalidRange // Parsed invalid ranges.
	reverse            bool           // Whether to generate Reverse{{Type}} function.
	marshal            bool           // Whether to generate MarshalJSON/UnmarshalJSON methods.
	marshalinsensitive bool           // Whether to make UnmarshalJSON case-insensitive.
	list               bool           // Whether to generate List{{Type}}() iter.Seq function.
	replacements       []ReplacePair  // String replacements for Reverse function.

	logf func(format string, args ...any) // test logging hook; nil when not testing
}

func (g *Generator) Printf(format string, args ...any) {
	fmt.Fprintf(&g.buf, format, args...)
}

// parseInvalidRanges parses the -invalid flag value into InvalidRange structs.
// It validates that ranges don't overlap and filters out negative values for unsigned types.
func (g *Generator) parseInvalidRanges(typeName string, isSigned bool) error {
	if g.invalidFlag == "" {
		return nil
	}

	g.invalidRanges = nil
	specs := strings.Split(g.invalidFlag, ",")

	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}

		var ir InvalidRange
		var err error

		switch {
		case strings.HasPrefix(spec, ">="):
			ir.op = ">="
			ir.value, err = strconv.ParseInt(strings.TrimSpace(spec[2:]), 10, 64)
		case strings.HasPrefix(spec, "<="):
			ir.op = "<="
			ir.value, err = strconv.ParseInt(strings.TrimSpace(spec[2:]), 10, 64)
		case strings.HasPrefix(spec, ">"):
			ir.op = ">"
			ir.value, err = strconv.ParseInt(strings.TrimSpace(spec[1:]), 10, 64)
		case strings.HasPrefix(spec, "<"):
			ir.op = "<"
			ir.value, err = strconv.ParseInt(strings.TrimSpace(spec[1:]), 10, 64)
		default:
			// Just a number
			ir.op = ""
			ir.value, err = strconv.ParseInt(spec, 10, 64)
		}

		if err != nil {
			return fmt.Errorf("invalid value specification %q: %v", spec, err)
		}

		// Skip negative values for unsigned types
		if !isSigned && ir.value < 0 {
			if g.logf != nil {
				g.logf("skipping negative value %d for unsigned type %s", ir.value, typeName)
			}
			continue
		}

		g.invalidRanges = append(g.invalidRanges, ir)
	}

	// Validate no overlapping ranges
	return g.validateNoOverlap()
}

// validateNoOverlap checks that invalid ranges don't overlap.
func (g *Generator) validateNoOverlap() error {
	for i := 0; i < len(g.invalidRanges); i++ {
		for j := i + 1; j < len(g.invalidRanges); j++ {
			r1, r2 := g.invalidRanges[i], g.invalidRanges[j]

			// Check for overlaps between different range types
			if overlaps(r1, r2) {
				return fmt.Errorf("overlapping invalid ranges: %s and %s", formatRange(r1), formatRange(r2))
			}
		}
	}
	return nil
}

// parseReplacementPair splits a replacement string on unescaped commas.
// Returns (old, new, error). Supports backslash escaping:
//   \, → literal comma
//   \\ → literal backslash
//   \x → literal \x (for forward compatibility)
func parseReplacementPair(s string) (string, string, error) {
	var parts []string
	var current strings.Builder

	escaped := false
	for _, ch := range s {
		if escaped {
			// After backslash, all characters are literal
			current.WriteRune(ch)
			escaped = false
			continue
		}

		if ch == '\\' {
			escaped = true
			continue
		}

		if ch == ',' {
			// Unescaped comma - split here
			parts = append(parts, current.String())
			current.Reset()
			continue
		}

		current.WriteRune(ch)
	}

	// Add final part
	if escaped {
		// Trailing backslash is treated as literal backslash
		current.WriteRune('\\')
	}
	parts = append(parts, current.String())

	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid replacement format %q: expected \"old,new\"", s)
	}

	return parts[0], parts[1], nil
}

// parseReplacements parses the --replace flag values into ReplacePair structs.
// Each value should be in the format "old,new" with optional backslash escaping.
func (g *Generator) parseReplacements(replaceFlags []string) error {
	g.replacements = nil
	for _, r := range replaceFlags {
		old, new, err := parseReplacementPair(r)
		if err != nil {
			return err
		}

		g.replacements = append(g.replacements, ReplacePair{
			old: strings.TrimSpace(old),
			new: strings.TrimSpace(new),
		})
	}
	return nil
}

// overlaps checks if two InvalidRanges overlap.
func overlaps(r1, r2 InvalidRange) bool {
	// Same exact value
	if r1.op == "" && r2.op == "" && r1.value == r2.value {
		return true
	}

	// Value falls in a range
	if r1.op == "" && r2.op != "" {
		return valueInRange(r1.value, r2)
	}
	if r2.op == "" && r1.op != "" {
		return valueInRange(r2.value, r1)
	}

	// Two ranges - check for overlap
	// For simplicity, we'll be conservative and flag adjacent/touching ranges as overlapping
	switch {
	case r1.op == "<" && r2.op == ">":
		// < X and > Y overlap if Y < X
		return r2.value < r1.value
	case r1.op == ">" && r2.op == "<":
		return r1.value < r2.value
	case r1.op == "<=" && r2.op == ">=":
		return r2.value <= r1.value
	case r1.op == ">=" && r2.op == "<=":
		return r1.value <= r2.value
	case r1.op == r2.op:
		// Same operator - these don't really overlap, but might be redundant
		// We'll allow this for now
		return false
	}

	return false
}

// valueInRange checks if a value falls within a range.
func valueInRange(val int64, r InvalidRange) bool {
	switch r.op {
	case "<":
		return val < r.value
	case "<=":
		return val <= r.value
	case ">":
		return val > r.value
	case ">=":
		return val >= r.value
	}
	return false
}

// formatRange formats an InvalidRange for error messages.
func formatRange(r InvalidRange) string {
	if r.op == "" {
		return fmt.Sprintf("%d", r.value)
	}
	return fmt.Sprintf("%s %d", r.op, r.value)
}

// File holds a single parsed file and associated data.
type File struct {
	pkg  *Package  // Package to which this file belongs.
	file *ast.File // Parsed AST.
	// These fields are reset for each type being generated.
	typeName string  // Name of the constant type.
	values   []Value // Accumulator for constant values of that type.

	trimPrefix  string
	lineComment bool
}

type Package struct {
	name         string
	defs         map[*ast.Ident]types.Object
	files        []*File
	hasTestFiles bool
}

// loadPackages analyzes the single package constructed from the patterns and tags.
// loadPackages exits if there is an error.
//
// Returns all variants (such as tests) of the package.
//
// logf is a test logging hook. It can be nil when not testing.
func loadPackages(
	patterns, tags []string,
	trimPrefix string, lineComment bool,
	logf func(format string, args ...any),
) []*Package {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedFiles,
		// Tests are included, let the caller decide how to fold them in.
		Tests:      true,
		BuildFlags: []string{fmt.Sprintf("-tags=%s", strings.Join(tags, " "))},
		Logf:       logf,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		log.Fatal(err)
	}
	if len(pkgs) == 0 {
		log.Fatalf("error: no packages matching %v", strings.Join(patterns, " "))
	}

	out := make([]*Package, len(pkgs))
	for i, pkg := range pkgs {
		p := &Package{
			name:  pkg.Name,
			defs:  pkg.TypesInfo.Defs,
			files: make([]*File, len(pkg.Syntax)),
		}

		for j, file := range pkg.Syntax {
			p.files[j] = &File{
				file: file,
				pkg:  p,

				trimPrefix:  trimPrefix,
				lineComment: lineComment,
			}
		}

		// Keep track of test files, since we might want to generated
		// code that ends up in that kind of package.
		// Can be replaced once https://go.dev/issue/38445 lands.
		for _, f := range pkg.GoFiles {
			if strings.HasSuffix(f, "_test.go") {
				p.hasTestFiles = true
				break
			}
		}

		out[i] = p
	}
	return out
}

func findValues(typeName string, pkg *Package) []Value {
	values := make([]Value, 0, 100)
	for _, file := range pkg.files {
		// Set the state for this run of the walker.
		file.typeName = typeName
		file.values = nil
		if file.file != nil {
			ast.Inspect(file.file, file.genDecl)
			values = append(values, file.values...)
		}
	}
	return values
}

// generate produces the String method for the named type.
func (g *Generator) generate(typeName string, values []Value) {
	// Generate code that will fail if the constants change value.
	g.Printf("func _() {\n")
	g.Printf("\t// An \"invalid array index\" compiler error signifies that the constant values have changed.\n")
	g.Printf("\t// Re-run the stringer command to generate them again.\n")
	g.Printf("\tvar x [1]struct{}\n")
	for _, v := range values {
		g.Printf("\t_ = x[%s - %s]\n", v.originalName, v.str)
	}
	g.Printf("}\n")
	runs := splitIntoRuns(values)
	// The decision of which pattern to use depends on the number of
	// runs in the numbers. If there's only one, it's easy. For more than
	// one, there's a tradeoff between complexity and size of the data
	// and code vs. the simplicity of a map. A map takes more space,
	// but so does the code. The decision here (crossover at 10) is
	// arbitrary, but considers that for large numbers of runs the cost
	// of the linear scan in the switch might become important, and
	// rather than use yet another algorithm such as binary search,
	// we punt and use a map. In any case, the likelihood of a map
	// being necessary for any realistic example other than bitmasks
	// is very low. And bitmasks probably deserve their own analysis,
	// to be done some other day.
	switch {
	case len(runs) == 1:
		g.buildOneRun(runs, typeName)
	case len(runs) <= 10:
		g.buildMultipleRuns(runs, typeName)
	default:
		g.buildMap(runs, typeName)
	}
	// Parse invalid ranges if specified (needed for both Valid() and Reverse functions)
	if g.invalidFlag != "" || g.valid {
		// Determine if the type is signed
		isSigned := len(values) > 0 && values[0].signed

		// Parse invalid ranges
		if err := g.parseInvalidRanges(typeName, isSigned); err != nil {
			log.Fatalf("parsing invalid ranges for %s: %v", typeName, err)
		}
	}

	// Generate Valid() method if requested.
	if g.valid {
		switch {
		case len(runs) == 1:
			g.buildValidOneRun(runs, typeName)
		case len(runs) <= 10:
			g.buildValidMultipleRuns(runs, typeName)
		default:
			g.buildValidMap(runs, typeName)
		}
	}
	// Generate Reverse{{Type}} function if requested.
	if g.reverse {
		g.buildReverseMaps(runs, typeName)
		g.buildReverseFunc(typeName)
	}
	// Generate MarshalJSON and UnmarshalJSON methods if requested.
	if g.marshal {
		g.buildMarshalMethods(typeName)
	}
	// Generate List{{Type}} function if requested.
	if g.list {
		g.buildListFunc(runs, typeName)
	}
}

// splitIntoRuns breaks the values into runs of contiguous sequences.
// For example, given 1,2,3,5,6,7 it returns {1,2,3},{5,6,7}.
// The input slice is known to be non-empty.
func splitIntoRuns(values []Value) [][]Value {
	// We use stable sort so the lexically first name is chosen for equal elements.
	sort.Stable(byValue(values))
	// Remove duplicates. Stable sort has put the one we want to print first,
	// so use that one. The String method won't care about which named constant
	// was the argument, so the first name for the given value is the only one to keep.
	// We need to do this because identical values would cause the switch or map
	// to fail to compile.
	j := 1
	for i := 1; i < len(values); i++ {
		if values[i].value != values[i-1].value {
			values[j] = values[i]
			j++
		}
	}
	values = values[:j]
	runs := make([][]Value, 0, 10)
	for len(values) > 0 {
		// One contiguous sequence per outer loop.
		i := 1
		for i < len(values) && values[i].value == values[i-1].value+1 {
			i++
		}
		runs = append(runs, values[:i])
		values = values[i:]
	}
	return runs
}

// format returns the gofmt-ed contents of the Generator's buffer.
func (g *Generator) format() []byte {
	src, err := format.Source(g.buf.Bytes())
	if err != nil {
		// Should never happen, but can arise when developing this code.
		// The user can compile the output to see the error.
		log.Printf("warning: internal error: invalid Go generated: %s", err)
		log.Printf("warning: compile the package to analyze the error")
		return g.buf.Bytes()
	}
	return src
}

// Value represents a declared constant.
type Value struct {
	originalName string // The name of the constant.
	name         string // The name with trimmed prefix.
	// The value is stored as a bit pattern alone. The boolean tells us
	// whether to interpret it as an int64 or a uint64; the only place
	// this matters is when sorting.
	// Much of the time the str field is all we need; it is printed
	// by Value.String.
	value  uint64 // Will be converted to int64 when needed.
	signed bool   // Whether the constant is a signed type.
	str    string // The string representation given by the "go/constant" package.
}

func (v *Value) String() string {
	return v.str
}

// byValue lets us sort the constants into increasing order.
// We take care in the Less method to sort in signed or unsigned order,
// as appropriate.
type byValue []Value

func (b byValue) Len() int      { return len(b) }
func (b byValue) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byValue) Less(i, j int) bool {
	if b[i].signed {
		return int64(b[i].value) < int64(b[j].value)
	}
	return b[i].value < b[j].value
}

// genDecl processes one declaration clause.
func (f *File) genDecl(node ast.Node) bool {
	decl, ok := node.(*ast.GenDecl)
	if !ok || decl.Tok != token.CONST {
		// We only care about const declarations.
		return true
	}
	// The name of the type of the constants we are declaring.
	// Can change if this is a multi-element declaration.
	typ := ""
	// Loop over the elements of the declaration. Each element is a ValueSpec:
	// a list of names possibly followed by a type, possibly followed by values.
	// If the type and value are both missing, we carry down the type (and value,
	// but the "go/types" package takes care of that).
	for _, spec := range decl.Specs {
		vspec := spec.(*ast.ValueSpec) // Guaranteed to succeed as this is CONST.
		if vspec.Type == nil && len(vspec.Values) > 0 {
			// "X = 1". With no type but a value. If the constant is untyped,
			// skip this vspec and reset the remembered type.
			typ = ""

			// If this is a simple type conversion, remember the type.
			// We don't mind if this is actually a call; a qualified call won't
			// be matched (that will be SelectorExpr, not Ident), and only unusual
			// situations will result in a function call that appears to be
			// a type conversion.
			ce, ok := vspec.Values[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			id, ok := ce.Fun.(*ast.Ident)
			if !ok {
				continue
			}
			typ = id.Name
		}
		if vspec.Type != nil {
			// "X T". We have a type. Remember it.
			ident, ok := vspec.Type.(*ast.Ident)
			if !ok {
				continue
			}
			typ = ident.Name
		}
		if typ != f.typeName {
			// This is not the type we're looking for.
			continue
		}
		// We now have a list of names (from one line of source code) all being
		// declared with the desired type.
		// Grab their names and actual values and store them in f.values.
		for _, name := range vspec.Names {
			if name.Name == "_" {
				continue
			}
			// This dance lets the type checker find the values for us. It's a
			// bit tricky: look up the object declared by the name, find its
			// types.Const, and extract its value.
			obj, ok := f.pkg.defs[name]
			if !ok {
				log.Fatalf("no value for constant %s", name)
			}
			info := obj.Type().Underlying().(*types.Basic).Info()
			if info&types.IsInteger == 0 {
				log.Fatalf("can't handle non-integer constant type %s", typ)
			}
			value := obj.(*types.Const).Val() // Guaranteed to succeed as this is CONST.
			if value.Kind() != constant.Int {
				log.Fatalf("can't happen: constant is not an integer %s", name)
			}
			i64, isInt := constant.Int64Val(value)
			u64, isUint := constant.Uint64Val(value)
			if !isInt && !isUint {
				log.Fatalf("internal error: value of %s is not an integer: %s", name, value.String())
			}
			if !isInt {
				u64 = uint64(i64)
			}
			v := Value{
				originalName: name.Name,
				value:        u64,
				signed:       info&types.IsUnsigned == 0,
				str:          value.String(),
			}
			if c := vspec.Comment; f.lineComment && c != nil && len(c.List) == 1 {
				v.name = strings.TrimSpace(c.Text())
			} else {
				v.name = strings.TrimPrefix(v.originalName, f.trimPrefix)
			}
			f.values = append(f.values, v)
		}
	}
	return false
}

// Helpers

// usize returns the number of bits of the smallest unsigned integer
// type that will hold n. Used to create the smallest possible slice of
// integers to use as indexes into the concatenated strings.
func usize(n int) int {
	switch {
	case n < 1<<8:
		return 8
	case n < 1<<16:
		return 16
	default:
		// 2^32 is enough constants for anyone.
		return 32
	}
}

// declareIndexAndNameVars declares the index slices and concatenated names
// strings representing the runs of values.
func (g *Generator) declareIndexAndNameVars(runs [][]Value, typeName string) {
	var indexes, names []string
	for i, run := range runs {
		index, name := g.createIndexAndNameDecl(run, typeName, fmt.Sprintf("_%d", i))
		if len(run) != 1 {
			indexes = append(indexes, index)
		}
		names = append(names, name)
	}
	g.Printf("const (\n")
	for _, name := range names {
		g.Printf("\t%s\n", name)
	}
	g.Printf(")\n\n")

	if len(indexes) > 0 {
		g.Printf("var (")
		for _, index := range indexes {
			g.Printf("\t%s\n", index)
		}
		g.Printf(")\n\n")
	}
}

// declareIndexAndNameVar is the single-run version of declareIndexAndNameVars
func (g *Generator) declareIndexAndNameVar(run []Value, typeName string) {
	index, name := g.createIndexAndNameDecl(run, typeName, "")
	g.Printf("const %s\n", name)
	g.Printf("var %s\n", index)
}

// createIndexAndNameDecl returns the pair of declarations for the run. The caller will add "const" and "var".
func (g *Generator) createIndexAndNameDecl(run []Value, typeName string, suffix string) (string, string) {
	b := new(bytes.Buffer)
	indexes := make([]int, len(run))
	for i := range run {
		b.WriteString(run[i].name)
		indexes[i] = b.Len()
	}
	nameConst := fmt.Sprintf("_%s_name%s = %q", typeName, suffix, b.String())
	nameLen := b.Len()
	b.Reset()
	fmt.Fprintf(b, "_%s_index%s = [...]uint%d{0, ", typeName, suffix, usize(nameLen))
	for i, v := range indexes {
		if i > 0 {
			fmt.Fprintf(b, ", ")
		}
		fmt.Fprintf(b, "%d", v)
	}
	fmt.Fprintf(b, "}")
	return b.String(), nameConst
}

// declareNameVars declares the concatenated names string representing all the values in the runs.
func (g *Generator) declareNameVars(runs [][]Value, typeName string, suffix string) {
	g.Printf("const _%s_name%s = \"", typeName, suffix)
	for _, run := range runs {
		for i := range run {
			g.Printf("%s", run[i].name)
		}
	}
	g.Printf("\"\n")
}

// buildOneRun generates the variables and String method for a single run of contiguous values.
func (g *Generator) buildOneRun(runs [][]Value, typeName string) {
	values := runs[0]
	g.Printf("\n")
	g.declareIndexAndNameVar(values, typeName)

	g.Printf("func (i %s) String() string {\n", typeName)
	g.Printf("\tidx := int(i) - %s\n", values[0].String())

	// For unsigned types, skip the i < lower_bound check since it's always false
	if values[0].signed {
		g.Printf("\tif i < %s || idx >= len(_%s_index)-1 {\n", values[0].String(), typeName)
	} else {
		g.Printf("\tif idx >= len(_%s_index)-1 {\n", typeName)
	}

	g.Printf("\t\treturn \"%s(\" + strconv.FormatInt(int64(i), 10) + \")\"\n", typeName)
	g.Printf("\t}\n")
	g.Printf("\treturn _%s_name[_%s_index[idx] : _%s_index[idx+1]]\n", typeName, typeName, typeName)
	g.Printf("}\n")
}

// buildMultipleRuns generates the variables and String method for multiple runs of contiguous values.
// For this pattern, a single Printf format won't do.
func (g *Generator) buildMultipleRuns(runs [][]Value, typeName string) {
	g.Printf("\n")
	g.declareIndexAndNameVars(runs, typeName)
	g.Printf("func (i %s) String() string {\n", typeName)
	g.Printf("\tswitch {\n")
	for i, values := range runs {
		if len(values) == 1 {
			g.Printf("\tcase i == %s:\n", &values[0])
			g.Printf("\t\treturn _%s_name_%d\n", typeName, i)
			continue
		}
		if values[0].value == 0 && !values[0].signed {
			// For an unsigned lower bound of 0, "0 <= i" would be redundant.
			g.Printf("\tcase i <= %s:\n", &values[len(values)-1])
		} else {
			g.Printf("\tcase %s <= i && i <= %s:\n", &values[0], &values[len(values)-1])
		}
		if values[0].value != 0 {
			g.Printf("\t\ti -= %s\n", &values[0])
		}
		g.Printf("\t\treturn _%s_name_%d[_%s_index_%d[i]:_%s_index_%d[i+1]]\n",
			typeName, i, typeName, i, typeName, i)
	}
	g.Printf("\tdefault:\n")
	g.Printf("\t\treturn \"%s(\" + strconv.FormatInt(int64(i), 10) + \")\"\n", typeName)
	g.Printf("\t}\n")
	g.Printf("}\n")
}

// buildMap handles the case where the space is so sparse a map is a reasonable fallback.
// It's a rare situation but has simple code.
func (g *Generator) buildMap(runs [][]Value, typeName string) {
	g.Printf("\n")
	g.declareNameVars(runs, typeName, "")
	g.Printf("\nvar _%s_map = map[%s]string{\n", typeName, typeName)
	n := 0
	for _, values := range runs {
		for _, value := range values {
			g.Printf("\t%s: _%s_name[%d:%d],\n", &value, typeName, n, n+len(value.name))
			n += len(value.name)
		}
	}
	g.Printf("}\n\n")
	g.Printf(stringMap, typeName)
}

// Argument to format is the type name.
const stringMap = `func (i %[1]s) String() string {
	if str, ok := _%[1]s_map[i]; ok {
		return str
	}
	return "%[1]s(" + strconv.FormatInt(int64(i), 10) + ")"
}
`

// buildValidOneRun generates the Valid method for a single run of contiguous values.
func (g *Generator) buildValidOneRun(runs [][]Value, typeName string) {
	values := runs[0]
	g.Printf("\n")
	g.Printf("func (i %s) Valid() bool {\n", typeName)

	// Add checks for invalid ranges first
	g.printInvalidRangeChecks()

	// Then check if it's in the valid range
	g.Printf("\tidx := int(i) - %s\n", values[0].String())

	// For unsigned types, skip the i < lower_bound check since it's always false
	if values[0].signed {
		g.Printf("\tif i < %s || idx >= len(_%s_index)-1 {\n", values[0].String(), typeName)
	} else {
		g.Printf("\tif idx >= len(_%s_index)-1 {\n", typeName)
	}

	g.Printf("\t\treturn false\n")
	g.Printf("\t}\n")
	g.Printf("\treturn true\n")
	g.Printf("}\n")
}

// printInvalidRangeChecks generates code to check if a value is in an invalid range.
func (g *Generator) printInvalidRangeChecks() {
	if len(g.invalidRanges) == 0 {
		return
	}

	for _, ir := range g.invalidRanges {
		switch ir.op {
		case "":
			g.Printf("\tif i == %d {\n", ir.value)
		case "<":
			g.Printf("\tif i < %d {\n", ir.value)
		case "<=":
			g.Printf("\tif i <= %d {\n", ir.value)
		case ">":
			g.Printf("\tif i > %d {\n", ir.value)
		case ">=":
			g.Printf("\tif i >= %d {\n", ir.value)
		}
		g.Printf("\t\treturn false\n")
		g.Printf("\t}\n")
	}
}

// buildValidMultipleRuns generates the Valid method for multiple runs of contiguous values.
func (g *Generator) buildValidMultipleRuns(runs [][]Value, typeName string) {
	g.Printf("\n")
	g.Printf("func (i %s) Valid() bool {\n", typeName)

	// Add checks for invalid ranges first
	g.printInvalidRangeChecks()

	g.Printf("\tswitch {\n")
	for _, values := range runs {
		if len(values) == 1 {
			g.Printf("\tcase i == %s:\n", &values[0])
			g.Printf("\t\treturn true\n")
			continue
		}
		if values[0].value == 0 && !values[0].signed {
			// For an unsigned lower bound of 0, "0 <= i" would be redundant.
			g.Printf("\tcase i <= %s:\n", &values[len(values)-1])
		} else {
			g.Printf("\tcase %s <= i && i <= %s:\n", &values[0], &values[len(values)-1])
		}
		g.Printf("\t\treturn true\n")
	}
	g.Printf("\tdefault:\n")
	g.Printf("\t\treturn false\n")
	g.Printf("\t}\n")
	g.Printf("}\n")
}

// buildValidMap generates the Valid method for sparse value sets using a map.
func (g *Generator) buildValidMap(runs [][]Value, typeName string) {
	g.Printf("\n")
	g.Printf("func (i %s) Valid() bool {\n", typeName)

	// Add checks for invalid ranges first
	g.printInvalidRangeChecks()

	g.Printf("\t_, ok := _%s_map[i]\n", typeName)
	g.Printf("\treturn ok\n")
	g.Printf("}\n")
}

// buildReverseMaps generates the reverse lookup maps for string to value conversion.
func (g *Generator) buildReverseMaps(runs [][]Value, typeName string) {
	g.Printf("\n")
	g.Printf("var _%s_rindex = map[string]%s{\n", typeName, typeName)
	for _, values := range runs {
		for _, value := range values {
			g.Printf("\t%q: %s,\n", value.name, value.originalName)
		}
	}
	g.Printf("}\n\n")

	g.Printf("var _%s_rindex_insensitive = map[string]%s{\n", typeName, typeName)
	for _, values := range runs {
		for _, value := range values {
			g.Printf("\t%q: %s,\n", strings.ToLower(value.name), value.originalName)
		}
	}
	g.Printf("}\n")
}

// buildReverseFunc generates the Reverse{{Type}} function for string to value lookup.
func (g *Generator) buildReverseFunc(typeName string) {
	g.Printf("\n")
	g.Printf("func Reverse%s(s string, caseSensitive bool) (%s, bool) {\n", typeName, typeName)

	// Apply string replacements if any
	for _, r := range g.replacements {
		g.Printf("\ts = strings.ReplaceAll(s, %q, %q)\n", r.old, r.new)
	}

	g.Printf("\tif caseSensitive {\n")
	g.Printf("\t\tif val, ok := _%s_rindex[s]; ok {\n", typeName)

	// If invalid ranges are specified, use Valid() to check
	if len(g.invalidRanges) > 0 {
		g.Printf("\t\t\treturn val, val.Valid()\n")
	} else {
		g.Printf("\t\t\treturn val, true\n")
	}

	g.Printf("\t\t}\n")
	g.Printf("\t} else {\n")
	g.Printf("\t\tif val, ok := _%s_rindex_insensitive[strings.ToLower(s)]; ok {\n", typeName)

	// If invalid ranges are specified, use Valid() to check
	if len(g.invalidRanges) > 0 {
		g.Printf("\t\t\treturn val, val.Valid()\n")
	} else {
		g.Printf("\t\t\treturn val, true\n")
	}

	g.Printf("\t\t}\n")
	g.Printf("\t}\n")
	g.Printf("\tvar zero %s\n", typeName)
	g.Printf("\treturn zero, false\n")
	g.Printf("}\n")
}

// buildListFunc generates the List{{Type}}() iter.Seq[{{Type}}] function.
func (g *Generator) buildListFunc(runs [][]Value, typeName string) {
	g.Printf("\n")
	g.Printf("func List%s() iter.Seq[%s] {\n", typeName, typeName)
	g.Printf("\treturn func(yield func(%s) bool) {\n", typeName)
	for _, values := range runs {
		for _, value := range values {
			g.Printf("\t\tif !yield(%s) {\n", value.originalName)
			g.Printf("\t\t\treturn\n")
			g.Printf("\t\t}\n")
		}
	}
	g.Printf("\t}\n")
	g.Printf("}\n")
}

// buildMarshalMethods generates MarshalJSON and UnmarshalJSON methods.
func (g *Generator) buildMarshalMethods(typeName string) {
	// MarshalJSON method
	g.Printf("\n")
	g.Printf("func (i %s) MarshalJSON() ([]byte, error) {\n", typeName)
	g.Printf("\tif !i.Valid() {\n")
	g.Printf("\t\treturn nil, fmt.Errorf(\"invalid %s value: %%d\", i)\n", typeName)
	g.Printf("\t}\n")
	g.Printf("\ts := i.String()\n")
	g.Printf("\treturn []byte(`\"` + s + `\"`), nil\n")
	g.Printf("}\n")

	// UnmarshalJSON method
	g.Printf("\n")
	g.Printf("func (i *%s) UnmarshalJSON(data []byte) error {\n", typeName)
	g.Printf("\tif len(data) < 2 || data[0] != '\"' || data[len(data)-1] != '\"' {\n")
	g.Printf("\t\treturn fmt.Errorf(\"invalid JSON string for %s\")\n", typeName)
	g.Printf("\t}\n")
	g.Printf("\tvar s string\n")
	g.Printf("\tif len(data) > 2 {\n")
	g.Printf("\t\ts = unsafe.String(&data[1], len(data)-2)\n")
	g.Printf("\t}\n")
	caseSensitive := !g.marshalinsensitive
	g.Printf("\tval, ok := Reverse%s(s, %t)\n", typeName, caseSensitive)
	g.Printf("\tif !ok {\n")
	g.Printf("\t\treturn fmt.Errorf(\"invalid %s value: %%s\", s)\n", typeName)
	g.Printf("\t}\n")
	g.Printf("\t*i = val\n")
	g.Printf("\treturn nil\n")
	g.Printf("}\n")
}
