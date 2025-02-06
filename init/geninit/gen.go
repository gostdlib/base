package main

import (
	"flag"
	"os"
	"strings"
	"text/template"

	_ "embed"
)

var (
	pkgName  = flag.String("pkg", "", "package name for this init package, usually a company name")
	initName = flag.String("init", "", "name of the init function, usually Init")
)

//go:embed init.tmpl
var initTmpl string

type tmplArgs struct {
	PkgName string
	Initer  string
}

func main() {
	flag.Parse()

	if *pkgName == "" {
		panic("pkg flag is required")
	}
	if *initName == "" {
		panic("init flag is required")
	}

	if strings.ToLower(*pkgName) != *pkgName {
		panic("pkg flag must be lowercase")
	}
	if strings.Title(*initName) != *initName {
		panic("init flag must be title case")
	}
	args := tmplArgs{
		PkgName: *pkgName,
		Initer:  *initName,
	}

	tmpl := template.Must(template.New("init").Parse(initTmpl))
	if err := tmpl.Execute(os.Stdout, args); err != nil {
		panic(err)
	}
}
