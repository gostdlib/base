package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gostdlib/base/values/immutable/generator/immutable"
)

var structType = flag.String("type", "", "Name of the struct to make immutable")

type blah struct {
	Name string
}

func main() {
	flag.Parse()

	if *structType == "" {
		log.Fatal("You must provide a struct name using the -type flag")
	}

	// Collect all Go files in the current directory
	goFiles, err := filepath.Glob("*.go")
	if err != nil {
		log.Fatalf("Failed to list Go files: %v", err)
	}

	var builder bytes.Buffer

	// Process each Go file until the target struct is found
	found := false
	for _, file := range goFiles {
		if strings.HasSuffix(file, immutable.ImmutableSuffix) {
			continue
		}
		fs := token.NewFileSet()
		fileAst, err := parser.ParseFile(fs, file, nil, parser.ParseComments)
		if err != nil {
			log.Printf("Skipping file %s due to parsing error: %v\n", file, err)
			continue
		}

		if found, err = immutable.Generate(fileAst, fs, &builder, *structType); found {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
	}

	if !found {
		log.Fatalf("Struct %s not found in the provided files", *structType)
	}

	// Generate the output file name dynamically
	outputFileName := fmt.Sprintf("%s_immutable.go", *structType)

	// Write the output to the dynamically named file
	err = os.WriteFile(outputFileName, []byte(builder.String()), 0644)
	if err != nil {
		log.Fatalf("Failed to write output file: %v", err)
	}
	log.Printf("Immutable struct generated in %s", outputFileName)
}
