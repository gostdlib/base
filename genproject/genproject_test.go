package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTemplateDrift verifies that the templates re-export every exported name in the base package they wrap,
// so an addition to base/context or base/errors fails this test until the template is updated.
func TestTemplateDrift(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		baseDir string
		// omit lists base exports the template intentionally does not re-export.
		omit map[string]bool
	}{
		{
			name:    "Success: context.tmpl re-exports the exported surface of base/context",
			tmpl:    "tmpls/context.tmpl",
			baseDir: "../context",
			omit: map[string]bool{
				// ResetBackground is init machinery: init.Service() calls it on the base package directly,
				// and the generated Background() holds no cache of its own.
				"ResetBackground": true,
			},
		},
		{
			name:    "Success: errors.tmpl re-exports the exported surface of base/errors",
			tmpl:    "tmpls/errors.tmpl",
			baseDir: "../errors",
			omit: map[string]bool{
				// The request-to-string logging helpers are standalone utilities, not part of the
				// generated package's API.
				"RequestConversion": true,
				"RequestSwitchCase": true,
				"RequestToStr":      true,
				"ProtoRequestToStr": true,
				"HTTPRequestToStr":  true,
				"ObjectToStr":       true,
				"JSONRequest":       true,
			},
		},
	}

	for _, test := range tests {
		tmplNames, err := exportedNames(test.tmpl)
		if err != nil {
			t.Errorf("TestTemplateDrift(%s): got err == %s parsing template, want err == nil", test.name, err)
			continue
		}

		files, err := filepath.Glob(filepath.Join(test.baseDir, "*.go"))
		if err != nil {
			t.Errorf("TestTemplateDrift(%s): got err == %s globbing base package, want err == nil", test.name, err)
			continue
		}

		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			baseNames, err := exportedNames(file)
			if err != nil {
				t.Errorf("TestTemplateDrift(%s): got err == %s parsing %s, want err == nil", test.name, err, file)
				continue
			}
			for name := range baseNames {
				if test.omit[name] {
					continue
				}
				if !tmplNames[name] {
					t.Errorf("TestTemplateDrift(%s): base package exports %q (%s) but the template does not re-export it; add a wrapper or add it to the omit list", test.name, name, file)
				}
			}
		}
	}
}

// exportedNames parses a Go source file (the templates are valid Go) and returns its exported top-level names.
func exportedNames(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	f, err := parser.ParseFile(token.NewFileSet(), path, b, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	names := map[string]bool{}
	add := func(id *ast.Ident) {
		if id.IsExported() {
			names[id.Name] = true
		}
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				add(d.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add(s.Name)
				case *ast.ValueSpec:
					for _, id := range s.Names {
						add(id)
					}
				}
			}
		}
	}
	return names, nil
}
