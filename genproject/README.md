# Genproject

This is simply a tool to generate a base project for new code. It will create:

- an `errors` package that wraps our errors library and stdlib errors
- a `context` package that wraps our context library and stdlib context
- a `main.go` file that calls init.Service()

Simple go to the empty directory you want to start a project in and call this binary:
`genproject`

And the files and directories will be created for you.

If the directory is already inside a Go module, genproject then runs the finishing steps for you:

```sh
go get -tool github.com/gostdlib/base/values/generators/stringer
go generate ./...
go mod tidy
```

If there is no go.mod yet, run `go mod init <your module path>` followed by the commands above (genproject
prints them as a reminder).

The `go generate` step creates the String() methods for the errors package's Category and Type enums; the
package will not compile without it.
