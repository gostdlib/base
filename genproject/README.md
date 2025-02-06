# Genproject

This is simply a tool to generate a base project for new code. It will create:

- an `errors` package that wraps our errors library and stdlib errors
- a `context` package that wraps our context library and stdlib context
- a `main.go` file that calls init.Service()

Simple go to the empty directory you want to start a project in and call this binary:
`genproject`

And the files and directories will be created for you.
