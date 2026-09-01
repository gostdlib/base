// Package generate contains types and functions that can be used to generate immutable types from existing
// Go struct types.
package generate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/printer"
	"go/token"
	"log"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"unicode"
)

const (
	ImmutableSuffix     = "_immutable.go"
	ImmutableTestSuffix = "_immutable_test.go"
)

// Field represents metadata about a struct field. Public for templates.
type Field struct {
	PublicName  string // Original public field name
	PrivateName string // Private field name
	Type        string // Field type
	WasPublic   bool   // True if the original field was public
	Comment     string // Comment associated with the field
	IsImmutable bool   // True if the field is immutable.Map or immutable.Slice
	Wrapped     bool   // True if the generator turned a raw map/slice into an immutable type, so conversions must copy
	GenericType string // Generic type for immutable.Map or immutable.Slice
}

// Method represents a method to be copied to the immutable struct. Public for templates.
type Method struct {
	Name            string  // Method name
	Params          string  // Parameters list
	Results         string  // Return types
	Body            string  // Method body
	FullReceiver    string  // The exact receiver (e.g. "*Record[T]")
	NewReceiver     string  // The new receiver (e.g. "*ImRecord[T]")
	ReceiverVar     string  // Receiver var, aka (r *Record[T]) would be "r"
	StructName      string  // Original struct name
	ImmutableStruct string  // Immutable struct name
	GenericUsage    string  // Generic usage (e.g., [T])
	StructFields    []Field // Fields of the struct
}

// ImBody returns the method body with lowercased field references.
func (m Method) ImBody() string {
	return lowerFieldReferences(m.ReceiverVar, m.StructFields, m.Body)
}

// StructData holds the data needed to generate a struct and its methods. Public for templates.
type StructData struct {
	Package       string   // Package name
	Name          string   // The name of the struct (prepended with Im)
	OriginalName  string   // The original struct name
	Fields        []Field  // The fields of the struct
	Comment       string   // The original struct's comment
	GenericParams string   // Full generic parameter list (e.g., [T any])
	GenericUsage  string   // Generic usage (e.g., [T])
	Methods       []Method // The methods to be copied to the immutable struct
	Imports       []string // The imports needed for the struct
	UsesImmutable bool     // True if any field is an immutable.Map or immutable.Slice, requiring the immutable import
	Wraps         bool     // True if the generator wrapped a raw map or slice, so Immutable() hands ownership over
	CopyOnConvert bool     // True if Immutable() should copy the wrapped maps and slices instead of sharing them
}

// immutablePkg is the import path the generated file needs for immutable.Map and immutable.Slice.
const immutablePkg = "github.com/gostdlib/base/values/immutable"

// ImmutablePkg gives the struct template the immutable import path so the path is written down once.
func (s StructData) ImmutablePkg() string {
	return immutablePkg
}

var funcMap = template.FuncMap{
	"hasPrefix":     strings.HasPrefix,
	"trimSpace":     strings.TrimSpace,
	"docComment":    docComment,
	"inlineComment": inlineComment,
	"multiline":     multiline,
	"immutableExpr": immutableExpr,
}

// immutableExpr renders the expression that converts a field of the mutable struct into the field on the generated
// immutable struct. A wrapped map or slice is shared with the original by default, which is what makes Immutable()
// an ownership transfer; withCopy copies it instead so both values stay usable.
func immutableExpr(f Field, withCopy bool) string {
	src := "r." + f.PublicName
	if !f.Wrapped {
		return src
	}
	switch {
	case strings.HasPrefix(f.Type, "immutable.Map"):
		if withCopy {
			src = "immutable.CopyMap(" + src + ")"
		}
		return "immutable.NewMap[" + f.GenericType + "](" + src + ")"
	case strings.HasPrefix(f.Type, "immutable.Slice"):
		if withCopy {
			src = "immutable.CopySlice(" + src + ")"
		}
		return "immutable.NewSlice[" + f.GenericType + "](" + src + ")"
	}
	return src
}

// multiline reports whether a comment body spans more than one line.
func multiline(s string) bool {
	return strings.Contains(strings.TrimSpace(s), "\n")
}

// docComment renders a comment body in a documentation comment position, returning the complete "//" prefixed
// lines. ast.CommentGroup.Text() strips the "//" markers but keeps the line breaks, so a template that prefixes
// only the first line emits bare Go source for every line after it and the generated file does not parse.
// A line's leading whitespace is preserved: an indented line is a code block in a Go doc comment, and trimming it
// would turn an example in the original comment into prose on the generated type, getter and setter.
func docComment(s string) string {
	lines := blankTrim(strings.Split(s, "\n"))
	for i, line := range lines {
		line = strings.TrimRight(line, " \t")
		switch {
		case line == "":
			lines[i] = "//"
		case strings.HasPrefix(line, " "), strings.HasPrefix(line, "\t"):
			// An indented line is a code block. Prefixing it with "// " instead of "//" would add a space
			// gofmt then has to take back out.
			lines[i] = "//" + line
		default:
			lines[i] = "// " + line
		}
	}
	return strings.Join(lines, "\n")
}

// commentBody returns a comment group's text with its leading and trailing blank lines removed, and "" when there
// is no comment. strings.TrimSpace cannot be used for this: it strips the whole body's leading whitespace, which is
// the first line's indentation, so a comment opening with a code block would be flattened into prose.
func commentBody(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	return strings.Join(blankTrim(strings.Split(g.Text(), "\n")), "\n")
}

// blankTrim drops the leading and trailing blank lines of a comment body, which a doc comment must not have.
func blankTrim(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// inlineComment renders a comment body in a trailing comment position, where a line break cannot be represented.
// The lines are collapsed into one.
func inlineComment(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// structTemplate provides a template for the immutable struct we are generating.
var structTemplate = template.Must(template.New("struct").Funcs(funcMap).Parse(`
// Code generated by immutable tool. DO NOT EDIT.

package {{.Package}}

import (
	{{- if .UsesImmutable }}
	"{{ $.ImmutablePkg }}"
	{{- end }}
	{{ range .Imports }}
	"{{.}}"
	{{- end }}
)

// {{.Name}}{{.GenericParams}} is an immutable version of {{.OriginalName}}{{.GenericParams}}.
{{- if .Comment }}
{{ docComment .Comment }}
{{- end }}
type {{.Name}}{{.GenericParams}} struct {
{{- range .Fields }}
{{- if and .Comment (multiline .Comment) }}
{{ docComment .Comment }}
	{{.PrivateName}} {{ .Type }}
{{- else }}
	{{.PrivateName}} {{ .Type }} {{ if .Comment }}// {{ inlineComment .Comment }}{{ end }}
{{- end }}
{{- end }}
}
`))

// copyTemplate provides a template for a function that copies of a the struct we are
// generating an immutable version of.
var copyTemplate = template.Must(template.New("copyFunc").Funcs(funcMap).Parse(`
func copy{{.Name}}{{.GenericParams}}(s {{.Name}}{{.GenericUsage}}) {{.Name}}{{.GenericUsage}} {
	return s
}
`))

// methodCopyTemplate is a template for copying the methods that exist on the original struct
// to the new immutable struct.
var methodCopyTemplate = template.Must(template.New("methodCopy").Funcs(funcMap).Parse(`
// {{.Name}} is a copy of the original method from {{.StructName}}.
func (r {{.NewReceiver}}) {{.Name}}{{ if .Params }}({{.Params}}){{ else }}(){{ end }}{{ if .Results }} {{.Results}}{{ end }} {
    {{ trimSpace .ImBody}}
}
`))

// SkipFile reports whether a file in the target's directory is not an input to generation: a file this tool
// generated, or any test file. An external test package may legally declare its own type with the target's name,
// so parsing test files would fail generation with "declared in this file but is not a struct type" depending
// only on the order the files were listed in. Every reader of a package directory must use this one predicate so
// the tool and its tests cannot disagree about which files count.
func SkipFile(name string) bool {
	return strings.HasSuffix(name, ImmutableSuffix) || strings.HasSuffix(name, "_test.go")
}

// Args are the arguments to Generate.
type Args struct {
	// Node is the parsed file to search for Target.
	Node *ast.File
	// FS is the FileSet Node was parsed with.
	FS *token.FileSet
	// Builder receives the generated source.
	Builder *bytes.Buffer
	// Target names the struct to make immutable. Must be a valid Go identifier.
	Target string
	// CopyOnConvert makes the generated Immutable() copy the maps and slices it wraps instead of sharing them
	// with the original struct. The default, false, makes Immutable() an ownership transfer.
	CopyOnConvert bool
}

// validate reports whether the arguments are usable.
func (a Args) validate() error {
	switch {
	case a.Node == nil:
		return fmt.Errorf("Node is required")
	case a.FS == nil:
		return fmt.Errorf("FS is required")
	case a.Builder == nil:
		return fmt.Errorf("Builder is required")
	case !token.IsIdentifier(a.Target):
		return fmt.Errorf("Target %q is not a valid Go identifier", a.Target)
	}
	return nil
}

// Generate generates an immutable version of the target struct from the provided Go file.
// Returns true if the target struct was found and processed. The output is written to args.Builder.
func Generate(args Args) (bool, error) {
	if err := args.validate(); err != nil {
		return false, err
	}
	node, fs, builder, targetStruct, copyOnConvert := args.Node, args.FS, args.Builder, args.Target, args.CopyOnConvert

	var packageName string
	found := false

	var outerErr error
	var intermediate bytes.Buffer

	// topLevel holds the file's own declarations so a type declared inside a function body, which ast.Inspect
	// also visits, is not mistaken for the target.
	topLevel := make(map[ast.Decl]bool, len(node.Decls))
	for _, d := range node.Decls {
		topLevel[d] = true
	}

	ast.Inspect(node, func(n ast.Node) bool {
		// Stop at the first error too: ast.Inspect's false only skips a node's children, not its siblings, so
		// without this the walk would carry on and a later success could mask the failure that was recorded.
		if found || outerErr != nil {
			return false
		}

		if pkg, ok := n.(*ast.File); ok {
			packageName = pkg.Name.Name // Extract package name
		}

		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE || !topLevel[ast.Decl(genDecl)] {
			return true
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != targetStruct {
				continue
			}

			// Found the target struct
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				outerErr = fmt.Errorf("%s is declared in this file but is not a struct type", targetStruct)
				return false
			}

			// Extract the struct's comment
			structComment := commentBody(genDecl.Doc)

			genericParams, genericUsage := extractTypeParams(fs, typeSpec)
			immutableStructName := "Im" + targetStruct

			// Prepare fields data
			fields := []Field{}
			fieldMap := map[string]string{}

			for _, field := range structType.Fields.List {
				for _, fieldName := range field.Names {
					fieldType, genericType, wrapped := computeFieldType(fs, field)

					// Extract field comments
					fieldComment := commentBody(field.Doc)

					// Determine if the field is immutable. A wrapped field was just given the type by
					// computeFieldType; an unwrapped one only counts when its qualifier resolves to this
					// package, since another module's immutable mirror imported under the same name is a
					// different type and emitting our import for it would collide with theirs.
					isImmutable := wrapped || isImmutableType(fieldType, immutableName(node))

					// Determine if the field was public
					isPublic := strings.ToUpper(fieldName.Name[:1]) == fieldName.Name[:1]

					fields = append(fields, Field{
						PublicName:  fieldName.Name,
						PrivateName: toLowerCamelCase(fieldName.Name),
						Type:        fieldType,
						WasPublic:   isPublic,
						Comment:     fieldComment,
						IsImmutable: isImmutable,
						Wrapped:     wrapped,
						GenericType: genericType,
					})
					fieldMap[fieldName.Name] = fieldType
				}
			}

			// Detect if we have a field that was public but now matches another field that
			// was already private.  Like .Hello and .hello, which causes us to have a collision.
			// This is O(n^2) but we are assuming that the number of fields is small.
			for _, field := range fields {
				if !field.WasPublic {
					continue
				}
				if _, ok := fieldMap[field.PrivateName]; ok {
					outerErr = fmt.Errorf("cannot generate immutable version: field %s collides with another private field when converted to non-public", field.PublicName)
					return false
				}
			}

			// Extract methods
			methods, err := extractMethods(node, fs, targetStruct, fieldMap, fields)
			if err != nil {
				outerErr = fmt.Errorf("failed to extract methods: %w", err)
				return false
			}

			// Determine whether any field requires the immutable package import, and whether any raw map or
			// slice was wrapped, which is what makes Immutable() an ownership transfer.
			usesImmutable := false
			wraps := false
			for _, f := range fields {
				if f.IsImmutable {
					usesImmutable = true
				}
				if f.Wrapped {
					wraps = true
				}
			}

			// Prepare struct data
			data := StructData{
				Package:       packageName,
				Name:          immutableStructName,
				OriginalName:  targetStruct,
				Fields:        fields,
				Comment:       structComment,
				GenericParams: genericParams,
				GenericUsage:  genericUsage,
				Methods:       methods,
				UsesImmutable: usesImmutable,
				Wraps:         wraps,
				CopyOnConvert: copyOnConvert,
			}

			// Find any packages that the struct uses so we can import them. The template emits the immutable
			// import itself when UsesImmutable is set, so a struct that already declares an immutable field
			// would otherwise have it imported twice and the generated file would not compile.
			data.Imports, err = findStructImports(node, targetStruct)
			if err != nil {
				outerErr = fmt.Errorf("failed to find struct imports: %w", err)
				return false
			}
			if usesImmutable {
				data.Imports = slices.DeleteFunc(data.Imports, func(i string) bool { return i == immutablePkg })

				// The generated file refers to this generator's immutable package by the name "immutable", so a
				// different package carried in under that name cannot sit beside it. Only the imports the target
				// actually uses matter: one referenced elsewhere in the file never reaches the generated file.
				// Say so here rather than emitting both and leaving the caller with "redeclared in this block".
				if conflict := conflictingImmutable(node, data.Imports); conflict != "" {
					outerErr = fmt.Errorf("%s: %s uses %q as immutable, which collides with the %q the generated file needs; import one of them under another name", fs.Position(node.Pos()).Filename, targetStruct, conflict, immutablePkg)
					return false
				}
			}

			// Generate struct
			err = structTemplate.Execute(&intermediate, data)
			if err != nil {
				outerErr = fmt.Errorf("failed to execute struct template: %w", err)
				return false
			}

			// Generate methods
			err = methodTemplate.Execute(&intermediate, data)
			if err != nil {
				outerErr = fmt.Errorf("failed to execute method template: %w", err)
				return false
			}

			// Generate copy function
			err = copyTemplate.Execute(&intermediate, data)
			if err != nil {
				outerErr = fmt.Errorf("failed to execute copy template: %w", err)
				return false
			}

			for _, method := range methods {
				if err := methodCopyTemplate.Execute(&intermediate, method); err != nil {
					outerErr = fmt.Errorf("failed to execute method copy template: %w", err)
					return false
				}
			}

			found = true
			break
		}

		return true
	})

	if outerErr != nil {
		return false, outerErr
	}

	if found {
		formatted, err := format.Source(intermediate.Bytes())
		if err != nil {
			return false, fmt.Errorf("failed to format generated code: %w", err)
		}
		builder.Write(formatted)
	}

	return found, nil
}

func extractTypeParams(fs *token.FileSet, typeSpec *ast.TypeSpec) (params string, usage string) {
	// Extract generic parameters
	if typeSpec.TypeParams != nil {
		var params, usages []string
		for _, param := range typeSpec.TypeParams.List {
			var paramStr strings.Builder
			for _, name := range param.Names {
				paramStr.WriteString(name.Name)
				usages = append(usages, name.Name)
			}
			if param.Type != nil {
				paramStr.WriteString(" ")
				paramStr.WriteString(formatNode(fs, param.Type))
			}
			params = append(params, paramStr.String())
		}
		genericParams := "[" + strings.Join(params, ", ") + "]"
		genericUsage := "[" + strings.Join(usages, ", ") + "]"
		return genericParams, genericUsage
	}
	return "", ""
}

// formatNode formats the given AST node into a string using the provided file set.
func formatNode(fs *token.FileSet, node ast.Node) string {
	var buf strings.Builder

	if node == nil {
		return ""
	}

	switch n := node.(type) {
	case *ast.FieldList:
		// Handle FieldList by iterating over fields
		var fields []string
		for _, field := range n.List {
			var fieldNames []string
			for _, name := range field.Names {
				fieldNames = append(fieldNames, name.Name)
			}

			// Get the type of the field
			fieldType := formatNode(fs, field.Type)

			// Combine field names and type
			if len(fieldNames) > 0 {
				fields = append(fields, fmt.Sprintf("%s %s", strings.Join(fieldNames, ", "), fieldType))
			} else {
				fields = append(fields, fieldType)
			}
		}

		// Join all formatted fields with commas
		return strings.Join(fields, ", ")

	default:
		// Default case for other node types
		err := printer.Fprint(&buf, fs, node)
		if err != nil {
			log.Fatalf("Failed to format node: %v", err)
		}
	}

	return buf.String()
}

// computeFieldType returns the type the generated field gets, the generic arguments of that type, and whether a raw
// map or slice was wrapped into an immutable one. Wrapped reports the difference between a field the generator
// converted, whose conversions have to copy, and one the author already declared immutable, which is passed through.
func computeFieldType(fs *token.FileSet, field *ast.Field) (fieldType, genericType string, wrapped bool) {
	fieldType = formatNode(fs, field.Type)

	// Replace map and slice types with immutable versions.
	switch {
	case strings.HasPrefix(fieldType, "map["):
		bracketStart := strings.Index(fieldType, "[") + 1
		bracketEnd := strings.Index(fieldType, "]")
		keyType := fieldType[bracketStart:bracketEnd]
		valueType := fieldType[bracketEnd+1:]
		genericType = keyType + ", " + strings.TrimSpace(valueType)
		fieldType = "immutable.Map[" + genericType + "]"
		wrapped = true
	case strings.HasPrefix(fieldType, "[]"):
		elementType := strings.TrimSpace(fieldType[2:])
		genericType = elementType
		fieldType = "immutable.Slice[" + genericType + "]"
		wrapped = true
	}
	return fieldType, genericType, wrapped
}

// immutableName returns the name the file uses for this generator's immutable package, or "" when the file does
// not import it. Aliased imports of other packages are not carried into the generated file, so an aliased
// immutable import is not supported and correctly resolves to a field this generator leaves alone.
func immutableName(f *ast.File) string {
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		switch {
		case err != nil, path != immutablePkg:
			continue
		case imp.Name != nil:
			return imp.Name.Name
		}
		return "immutable"
	}
	return ""
}

// conflictingImmutable returns the path of an import the target struct carries into the generated file under the
// name "immutable" that is not this generator's immutable package, or "". Only one package can hold that name in
// the generated file, and only imports the struct actually uses matter — one referenced elsewhere in the file is
// never emitted, so it cannot collide.
func conflictingImmutable(f *ast.File, imports []string) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p == immutablePkg || !slices.Contains(imports, p) {
			continue
		}
		name := pathName(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "immutable" {
			return p
		}
	}
	return ""
}

// pathName guesses the package name an unaliased import resolves to: the last path element, skipping a major
// version suffix such as /v2. A package whose name differs from its directory cannot be recognized without type
// information; findStructImports makes the same assumption.
func pathName(p string) string {
	parts := strings.Split(p, "/")
	base := parts[len(parts)-1]
	if len(parts) > 1 && len(base) > 1 && base[0] == 'v' {
		if _, err := strconv.Atoi(base[1:]); err == nil {
			return parts[len(parts)-2]
		}
	}
	return base
}

// isImmutableType reports whether a field type written by hand is one of this package's immutable types, given
// the name the file imports that package under.
func isImmutableType(fieldType, name string) bool {
	if name == "" {
		return false
	}
	return strings.HasPrefix(fieldType, name+".Map[") || strings.HasPrefix(fieldType, name+".Slice[")
}

// lowerFieldReferences finds expressions of the form "<recvVar>.<Field>"
// and lowercases the first letter of <Field>, but *only* if <Field> is in fields
// (matching Field.PrivateName).  It leaves references to methods (or unknown fields)
// untouched.
func lowerFieldReferences(recvVar string, fields []Field, body string) string {
	// Compile a regex to match "<recvVar>.<Something>"
	// \b ensures that we match "r." as a separate token (not "otherr.Something").
	// ([A-Z][A-Za-z0-9_]*) captures the capitalized identifier (e.g., "Name", "UserID").
	//
	// Explanation:
	//   \b        : word boundary (beginning or end of word)
	//   recvVar   : (e.g., "r"), but we quote it in case it has special chars
	//   \.        : literal dot
	//   ([A-Z][A-Za-z0-9_]*) : group 1 captures an identifier that starts with uppercase
	//   \b        : word boundary again
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(recvVar) + `\.([A-Z][A-Za-z0-9_]*)\b`)

	return re.ReplaceAllStringFunc(body, func(match string) string {
		// match is something like "r.FieldName" or "r.UserID"
		submatches := re.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match // safety check
		}

		fieldName := submatches[1] // e.g. "FieldName"

		// If the captured name is NOT in our fields slice, skip.
		if !isFieldName(fields, fieldName) {
			return match
		}

		// fieldName is known to be a field. Lowercase just the first letter.
		lowerName := toLowerCamelCase(fieldName)
		// Rebuild "r.fieldName"
		return recvVar + "." + lowerName
	})
}

// isFieldName returns true if name matches the PrivateName of any Field in fields.
func isFieldName(fields []Field, name string) bool {
	name = toLowerCamelCase(name)
	for _, f := range fields {
		if f.PrivateName == name {
			return true
		}
	}
	return false
}

// isAllUpper returns true if every letter in s is uppercase.
// Non-letter runes (digits, symbols) will cause it to return false
// unless you adjust the logic as you see fit.
func isAllUpper(s []rune) bool {
	for _, r := range s {
		// If it's a letter and not uppercase, return false
		if unicode.IsLetter(r) && !unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

// toLowerCamelCase converts a string to "lower camelCase".
// If all letters in the string are uppercase, it lowercases everything.
func toLowerCamelCase(s string) string {
	if len(s) == 0 {
		return s
	}

	runes := []rune(s)

	// If the entire string is uppercase letters, just lowercase everything.
	if isAllUpper(runes) {
		return strings.ToLower(s)
	}

	// Lowercase the first letter if it’s uppercase.
	if unicode.IsUpper(runes[0]) {
		runes[0] = unicode.ToLower(runes[0])
	}

	// For the rest of the runes, detect consecutive uppercase sequences.
	for i := 1; i < len(runes); i++ {
		if unicode.IsUpper(runes[i]) {
			// If the next rune is also uppercase (and in range),
			// treat the current rune as part of an acronym => make it lowercase.
			// Otherwise do nothing (this uppercase might be a capital letter starting a “word”).
			if i+1 < len(runes) && unicode.IsUpper(runes[i+1]) {
				runes[i] = unicode.ToLower(runes[i])
			}

		}
	}

	return string(runes)
}
