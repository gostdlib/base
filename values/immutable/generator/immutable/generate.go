// Package immutable contains types and functions that can be used to generate immutable types from existing
// Go struct types.
package immutable

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"log"
	"regexp"
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
	ReceiverComment string  // Comment associated with the receiver
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
}

var funcMap = template.FuncMap{
	"hasPrefix": strings.HasPrefix,
	"trimSpace": strings.TrimSpace,
}

// structTemplate provides a template for the immutable struct we are generating.
var structTemplate = template.Must(template.New("struct").Funcs(funcMap).Parse(`
// Code generated by immutable tool. DO NOT EDIT.

package {{.Package}}

import (
	"github.com/gostdlib/base/values/immutable"
	{{ range .Imports }}
	"{{.}}"
	{{- end }}
)

// {{.Name}}{{.GenericParams}} is an immutable version of {{.OriginalName}}{{.GenericParams}}.
{{- if .Comment }}
// {{ .Comment }}
{{- end }}
type {{.Name}}{{.GenericParams}} struct {
{{- range .Fields }}
	{{.PrivateName}} {{ .Type }} {{ if .Comment }}// {{ .Comment }}{{ end }}
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

// Generate generates an immutable version of the target struct from the provided Go file.
// Returns true if the target struct was found and processed. The ouptut is written to the provided
// strings.Builder.
func Generate(node *ast.File, fs *token.FileSet, builder *bytes.Buffer, targetStruct string) (bool, error) {
	var packageName string
	found := false

	var outerErr error

	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}

		if pkg, ok := n.(*ast.File); ok {
			packageName = pkg.Name.Name // Extract package name
		}

		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
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
				log.Println("target struct is not a struct")
				continue
			}

			// Extract the struct's comment
			var structComment string
			if genDecl.Doc != nil {
				structComment = strings.TrimSpace(genDecl.Doc.Text())
			}

			genericParams, genericUsage := extractTypeParams(fs, typeSpec)
			immutableStructName := "Im" + targetStruct

			// Prepare fields data
			fields := []Field{}
			fieldMap := map[string]string{}

			for _, field := range structType.Fields.List {
				for _, fieldName := range field.Names {
					fieldType, genericType := computeFieldType(fs, field)

					// Extract field comments
					fieldComment := ""
					if field.Doc != nil {
						fieldComment = strings.TrimSpace(field.Doc.Text())
					}

					// Determine if the field is immutable
					isImmutable := strings.HasPrefix(fieldType, "immutable.Map") || strings.HasPrefix(fieldType, "immutable.Slice")

					// Determine if the field was public
					isPublic := strings.ToUpper(fieldName.Name[:1]) == fieldName.Name[:1]

					fields = append(fields, Field{
						PublicName:  fieldName.Name,
						PrivateName: toLowerCamelCase(fieldName.Name),
						Type:        fieldType,
						WasPublic:   isPublic,
						Comment:     fieldComment,
						IsImmutable: isImmutable,
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
			}

			// Find any packages that the struct uses so we can import them.
			data.Imports, err = findStructImports(node, targetStruct)
			if err != nil {
				outerErr = fmt.Errorf("failed to find struct imports: %w", err)
				return false
			}

			// Generate struct
			err = structTemplate.Execute(builder, data)
			if err != nil {
				outerErr = fmt.Errorf("failed to execute struct template: %w", err)
				return false
			}

			// Generate methods
			err = methodTemplate.Execute(builder, data)
			if err != nil {
				outerErr = fmt.Errorf("failed to execute method template: %w", err)
				return false
			}

			// Generate copy function
			err = copyTemplate.Execute(builder, data)
			if err != nil {
				outerErr = fmt.Errorf("failed to execute copy template: %w", err)
				return false
			}

			for _, method := range methods {
				methodCopyTemplate.Execute(builder, method)
			}

			found = true
			break
		}

		return true
	})

	if outerErr != nil {
		return false, outerErr
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

func computeFieldType(fs *token.FileSet, field *ast.Field) (fieldType, genericType string) {
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
	case strings.HasPrefix(fieldType, "[]"):
		elementType := strings.TrimSpace(fieldType[2:])
		genericType = elementType
		fieldType = "immutable.Slice[" + genericType + "]"
	}
	// Replace map and slice types with immutable versions
	return fieldType, genericType
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

// joinComments helper function to join comments from AST nodes
func joinComments(comments []*ast.CommentGroup) string {
	var lines []string
	for _, group := range comments {
		lines = append(lines, strings.TrimSpace(group.Text()))
	}
	return strings.Join(lines, "\n")
}
