package main

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"os"
	"text/template"
)

//go:embed tmpls/*.tmpl
var fs embed.FS

func main() {
	tmpls := template.Must(template.ParseFS(fs, "tmpls/*.tmpl"))

	if _, err := os.Stat("context"); !os.IsNotExist(err) {
		panic("context directory already exists")
	}
	if _, err := os.Stat("errors"); !os.IsNotExist(err) {
		panic("errors directory already exists")
	}
	if _, err := os.Stat("main.go"); !os.IsNotExist(err) {
		panic("main.go already exists")
	}

	if err := os.Mkdir("context", 0755); err != nil {
		panic(err)
	}
	if err := os.Mkdir("errors", 0755); err != nil {
		panic(err)
	}

	f, err := os.Create("context/context.go")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	formatWrite(f, tmpls, "context.tmpl")

	f, err = os.Create("errors/errors.go")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	formatWrite(f, tmpls, "errors.tmpl")

	f, err = os.Create("main.go")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	formatWrite(f, tmpls, "main.tmpl")

	fmt.Println("Finished. Remember to run `go generate ./...` before trying to compile.")
}

func formatWrite(f *os.File, tmpls *template.Template, tmpl string) {
	b := bytes.NewBuffer([]byte{})
	if err := tmpls.ExecuteTemplate(b, tmpl, nil); err != nil {
		panic(err)
	}
	formatted, err := format.Source(b.Bytes())
	if err != nil {
		panic(err)
	}
	if _, err := f.Write(formatted); err != nil {
		panic(err)
	}
}
