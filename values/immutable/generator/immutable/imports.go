package immutable

import (
	"go/ast"
	"go/token"
	"log"
	"path/filepath"
	"strings"
)

// findStructImports parses the given Go source file and finds
// all imports that are referenced by the specified struct and
// its methods. It returns a list of matched import paths.
func findStructImports(fileAst *ast.File, structName string) ([]string, error) {
	// Gather the imports in a map: aliasOrPackageName -> fullImportPath
	// e.g., if we have: import xyz "github.com/foo/bar"
	// then importsMap["xyz"] = "github.com/foo/bar"
	// if import is unaliased: import "net/http"
	// then importsMap["http"] = "net/http"
	importsMap := make(map[string]string)

	for _, imp := range fileAst.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)

		var importName string
		if imp.Name != nil && imp.Name.Name != "" {
			// e.g. import myalias "some/package"
			importName = imp.Name.Name
		} else {
			// e.g. import "some/package"
			// default alias is the last part of the package path
			base := filepath.Base(importPath)
			importName = strings.TrimSuffix(base, filepath.Ext(base))
		}

		importsMap[importName] = importPath
	}

	// A set to record discovered packages that the struct uses
	usedImports := make(map[string]bool)

	// 1) Find the struct declaration; gather references within fields
	// 2) Find all methods with the specified struct as receiver; gather references in method bodies
	for _, decl := range fileAst.Decls {
		// Check for a GenDecl (could contain type specs, import specs, etc.)
		genDecl, ok := decl.(*ast.GenDecl)
		if ok && genDecl.Tok == token.TYPE {
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				// Found a type spec with the same name?
				if typeSpec.Name == nil || typeSpec.Name.Name != structName {
					continue
				}
				// Check if it's actually a struct
				structType, ok := typeSpec.Type.(*ast.StructType)
				if ok {
					// Now examine fields of the struct
					if structType.Fields != nil {
						for _, field := range structType.Fields.List {
							// A field could have multiple names but the same type
							// The type is what might reference an imported package
							collectPackageRefs(field.Type, importsMap, usedImports)
						}
					}
				}
			}
		}

		// Check for a FuncDecl (could be a method or a regular function)
		funcDecl, ok := decl.(*ast.FuncDecl)
		if ok && funcDecl.Recv != nil && funcDecl.Name != nil {
			// Ensure the function body is not nil before processing
			if funcDecl.Body != nil {
				collectPackageRefs(funcDecl.Body, importsMap, usedImports)
			} else {
				log.Println("Empty function body")
			}
			// For a method, the receiver is in funcDecl.Recv.List
			// e.g. `func (r *MyStruct) MyMethod(...) { ... }`
			recv := funcDecl.Recv.List
			if len(recv) == 1 {
				// The receiver might be *structName or structName
				starExpr, isStar := recv[0].Type.(*ast.StarExpr)
				if isStar {
					switch x := starExpr.X.(type) {
					case *ast.Ident:
						// e.g. (*Record) – no type param
						if x.Name == structName {
							collectPackageRefs(funcDecl.Body, importsMap, usedImports)
						}

					case *ast.SelectorExpr:
						// e.g. (*somePkg.Record)
						if x.Sel.Name == structName {
							collectPackageRefs(funcDecl.Body, importsMap, usedImports)
						}

					case *ast.IndexExpr:
						// e.g. (*Record[T]) or (*somePkg.Record[T])
						// x.X is the “Record” or “somePkg.Record” part
						// x.Index is the type param “T” or even another expression
						collectIfGenericReceiverMatches(
							collectArgs{
								x.X,
								structName,
								importsMap,
								usedImports,
								funcDecl.Body,
							},
						)
					}
				} else {
					// e.g. (MyStruct)
					ident, isIdent := recv[0].Type.(*ast.Ident)
					if isIdent && ident.Name == structName {
						// Method on structName
						// og.Println("found method: ", funcDecl.Name.Name)
						collectPackageRefs(funcDecl.Body, importsMap, usedImports)
					}
				}
			}
		}
	}

	// Convert usedImports map to a sorted slice
	result := make([]string, 0, len(usedImports))
	for pkg := range usedImports {
		result = append(result, pkg)
	}
	return result, nil
}

type collectArgs struct {
	indexExpr   ast.Expr
	structName  string
	importsMap  map[string]string
	usedImports map[string]bool
	body        *ast.BlockStmt
}

func collectIfGenericReceiverMatches(args collectArgs) {

	switch base := args.indexExpr.(type) {
	case *ast.IndexExpr:
		// Handles single generic type like Generic[T]
		if checkReceiverName(base.X, args.structName) {
			collectPackageRefs(args.body, args.importsMap, args.usedImports)
		}
	case *ast.IndexListExpr:
		// Handles multiple generic types like Generic[T, X]
		if checkReceiverName(base.X, args.structName) {
			collectPackageRefs(args.body, args.importsMap, args.usedImports)
		}
	case *ast.Ident:
		// Non-generic receiver like MyStruct
		if base.Name == args.structName {
			collectPackageRefs(args.body, args.importsMap, args.usedImports)
		}
	case *ast.SelectorExpr:
		// Handles package alias like somePkg.Generic[T, X]
		if base.Sel.Name == args.structName {
			collectPackageRefs(args.body, args.importsMap, args.usedImports)
		}
	}
}

// Helper function to check if the receiver type matches the struct name
func checkReceiverName(expr ast.Expr, structName string) bool {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name == structName
	case *ast.SelectorExpr:
		return x.Sel.Name == structName
	}
	return false
}

func collectPackageRefs(node ast.Node, importsMap map[string]string, usedImports map[string]bool) {
	if node == nil {
		return
	}

	ast.Inspect(node, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			// Handles cases like fmt.Sprintf
			if ident, ok := n.X.(*ast.Ident); ok {
				if fullPath, exists := importsMap[ident.Name]; exists {
					usedImports[fullPath] = true
				}
			}

		case *ast.CallExpr:
			// Function call handling like fmt.Println()
			if selExpr, ok := n.Fun.(*ast.SelectorExpr); ok {
				if ident, isIdent := selExpr.X.(*ast.Ident); isIdent {
					if fullPath, exists := importsMap[ident.Name]; exists {
						usedImports[fullPath] = true
					}
				}
			}

		case *ast.ExprStmt:
			collectPackageRefs(n.X, importsMap, usedImports)

		case *ast.AssignStmt:
			for _, expr := range n.Rhs {
				collectPackageRefs(expr, importsMap, usedImports)
			}

		case *ast.Ident:
			if fullPath, exists := importsMap[n.Name]; exists {
				usedImports[fullPath] = true
			}
		}

		return true
	})
}
