package generate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"text/template"
)

// methodTemplate provides a template for the methods we are adding to both the immutable
// and original struct (getters/setters/...).
var methodTemplate = template.Must(template.New("methods").Funcs(funcMap).Parse(`
{{- range .Fields }}
{{- if .WasPublic }}
// Get{{.PublicName}} retrieves the content of the field {{.PublicName}}.
{{- if .Comment }}
// {{.Comment}}
{{- end }}
func (r *{{$.Name}}{{$.GenericUsage}}) Get{{.PublicName}}() {{.Type}} {
	return r.{{.PrivateName}}
}

// Set{{.PublicName}} returns a copy of the struct with the field {{.PublicName}} set to the new value.
{{- if .Comment }}
// {{.Comment}}
{{- end }}
func (r *{{$.Name}}{{$.GenericUsage}}) Set{{.PublicName}}(value {{.Type}}) {{$.Name}}{{$.GenericUsage}} {
	n := copy{{$.Name}}{{$.GenericUsage}}(*r)
	n.{{.PrivateName}} = value
	return n
}
{{- end }}
{{- end }}

// Mutable converts the immutable struct back to the original mutable struct.
func (r *{{.Name}}{{.GenericUsage}}) Mutable() {{.OriginalName}}{{.GenericUsage}} {
	return {{.OriginalName}}{{.GenericUsage}}{
{{- range .Fields }}
		{{.PublicName}}: {{if .IsImmutable}}r.{{.PrivateName}}.Copy(){{else}}r.{{.PrivateName}}{{end}},
{{- end }}
	}
}

// Immutable converts the mutable struct to the generated immutable struct.
func (r *{{.OriginalName}}{{.GenericUsage}}) Immutable() {{.Name}}{{.GenericUsage}} {
	return {{.Name}}{{.GenericUsage}}{
{{- range .Fields }}
		{{.PrivateName}}: {{if .IsImmutable}}immutable.New{{if hasPrefix .Type "immutable.Map"}}Map[{{.GenericType}}]{{else if hasPrefix .Type "immutable.Slice"}}Slice[{{.GenericType}}]{{end}}{{end}}(r.{{.PublicName}}),
{{- end }}
	}
}
`))

// extractMethods extracts methods from the provided AST node and creates a list of Method objects
// with a .FullReceiver that includes the Im prefix.
func extractMethods(node ast.Node, fs *token.FileSet, structName string, fieldMap map[string]string, fields []Field) ([]Method, error) {
	var methods []Method

	var err error
	ast.Inspect(node, func(n ast.Node) bool {
		funcDecl, ok := n.(*ast.FuncDecl)
		if !ok || funcDecl.Recv == nil || len(funcDecl.Recv.List) == 0 || len(funcDecl.Recv.List[0].Names) == 0 {
			return true
		}

		// This is the original node for something like `(*Record[T])`.
		recvNode := funcDecl.Recv.List[0].Type
		receiverVar := funcDecl.Recv.List[0].Names[0].Name
		fullReceiver := formatNode(fs, recvNode) // e.g. "*Record[T]"

		var receiverName string
		switch recv := recvNode.(type) {
		case *ast.StarExpr:
			switch x := recv.X.(type) {
			case *ast.Ident:
				receiverName = x.Name
			case *ast.IndexExpr:
				if ident, ok := x.X.(*ast.Ident); ok {
					receiverName = ident.Name
				}
			case *ast.IndexListExpr: // Handle multiple type parameters
				if ident, ok := x.X.(*ast.Ident); ok {
					receiverName = ident.Name
				}
			}
		case *ast.Ident:
			receiverName = recv.Name
		case *ast.IndexExpr:
			if ident, ok := recv.X.(*ast.Ident); ok {
				receiverName = ident.Name
			}
		case *ast.IndexListExpr: // Handle multiple type parameters
			if ident, ok := recv.X.(*ast.Ident); ok {
				receiverName = ident.Name
			}
		}

		// Only proceed if the struct name matches (to avoid collecting methods for other types)
		if receiverName != structName {
			return true
		}
		// Check for mutating fields ... (unchanged)
		if detectFieldMutation(funcDecl.Body, fieldMap, receiverVar) {
			err = fmt.Errorf("cannot generate immutable version: method %s mutates fields", funcDecl.Name.Name)
			return false
		}

		// Format the body
		var statementsBuf bytes.Buffer
		for _, stmt := range funcDecl.Body.List {
			if err = printer.Fprint(&statementsBuf, fs, stmt); err != nil {
				err = fmt.Errorf("failed to format statement: %w", err)
				return false
			}
			// Add a newline or semicolon to separate statements
			statementsBuf.WriteString("\n")
		}
		body := statementsBuf.String()

		// Convert funcDecl.Doc.List to []*ast.CommentGroup for joinComments
		var comments []*ast.CommentGroup
		if funcDecl.Doc != nil {
			comments = []*ast.CommentGroup{
				{List: funcDecl.Doc.List},
			}
		}

		var newReceiver = fullReceiver
		if _, ok := recvNode.(*ast.StarExpr); ok {
			newReceiver = "*" + "Im" + newReceiver[1:]
		} else {
			newReceiver = "Im" + newReceiver
		}

		params := ""
		if funcDecl.Type.Params != nil {
			params = formatNode(fs, funcDecl.Type.Params)
		}
		results := ""
		if funcDecl.Type.Results != nil {
			results = formatNode(fs, funcDecl.Type.Results)
		}

		methods = append(methods, Method{
			Name:            funcDecl.Name.Name,
			Params:          params,
			Results:         results,
			Body:            body,
			ReceiverComment: joinComments(comments),

			// Fill the new field with the entire receiver
			FullReceiver: fullReceiver,
			NewReceiver:  newReceiver,
			ReceiverVar:  receiverVar,

			// Fill these if you still want them in the future:
			StructName:      structName,
			ImmutableStruct: "Im" + structName,
			GenericUsage:    "", // or handle generics if you want
		})

		return true
	})
	if err != nil {
		return nil, err
	}

	for i, m := range methods {
		m.StructFields = fields
		methods[i] = m
	}
	return methods, nil
}

// detectFieldMutation reports whether a method body mutates a field of the receiver, which would make
// it unsafe to copy onto the immutable type. It catches direct assignment (r.Field = x, r.Field += x)
// and increment/decrement (r.Field++, r.Field--) through the receiver variable.
//
// It deliberately does not catch indirect mutations the generator cannot soundly prove: mutation through
// a pointer field (r.ptr.X = y), aliasing the receiver into another variable that is then mutated, or
// element assignment into a map/slice field (r.items[k] = v). Methods doing those can slip through; the
// immutable guarantee for collection fields instead rests on the immutable.Map/Slice field wrappers.
func detectFieldMutation(body *ast.BlockStmt, fieldMap map[string]string, receiverVar string) bool {
	mutated := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if isReceiverFieldRef(lhs, fieldMap, receiverVar) {
					mutated = true
					return false // Stop further inspection.
				}
			}
		case *ast.IncDecStmt:
			if isReceiverFieldRef(n.X, fieldMap, receiverVar) {
				mutated = true
				return false // Stop further inspection.
			}
		}
		return true
	})
	return mutated
}

// isReceiverFieldRef reports whether expr is a reference to a field of the receiver, i.e.
// "<receiverVar>.<Field>" where <Field> is a known field of the struct.
func isReceiverFieldRef(expr ast.Expr, fieldMap map[string]string, receiverVar string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, exists := fieldMap[selector.Sel.Name]
	return exists && ident.Name == receiverVar
}
