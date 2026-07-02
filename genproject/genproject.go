package main

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"strings"
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

	if !haveGoMod() {
		fmt.Println("Files generated. No go.mod was found, so before compiling you must run:")
		fmt.Println("\tgo mod init <your module path>")
		fmt.Println("\tgo get -tool github.com/gostdlib/base/values/generators/stringer")
		fmt.Println("\tgo generate ./...")
		fmt.Println("\tgo mod tidy")
		return
	}

	run("go", "get", "-tool", "github.com/gostdlib/base/values/generators/stringer")
	run("go", "generate", "./...")
	run("go", "mod", "tidy")
	fmt.Println("Finished.")
}

// haveGoMod reports if the current directory is inside a Go module.
func haveGoMod() bool {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return false
	}
	p := strings.TrimSpace(string(out))
	return p != "" && p != os.DevNull
}

// run executes the command, streaming its output, and panics if it fails.
func run(args ...string) {
	fmt.Println("Running:", strings.Join(args, " "))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic(fmt.Sprintf("%s: %v", strings.Join(args, " "), err))
	}
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
