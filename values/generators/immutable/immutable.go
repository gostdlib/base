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

	"github.com/gostdlib/base/values/generators/immutable/internal/generate"
)

var (
	structType = flag.String("type", "", "Name of the struct to make immutable")
	copyValues = flag.Bool("copy", false, "Copy maps and slices in Immutable() so the original struct stays usable; by default they are shared and Immutable() hands ownership over")
)

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

	// Process each Go file until the target struct is found. A file can legitimately fail while a later one
	// still holds the target — a build-constrained twin declaring the name as another kind, for instance, since
	// Glob knows nothing of build tags — so the first error is remembered rather than fatal, and it is reported
	// only if no file ends up supplying the struct.
	found := false
	var genErr error
	for _, file := range goFiles {
		if generate.SkipFile(file) {
			continue
		}
		fs := token.NewFileSet()
		fileAst, err := parser.ParseFile(fs, file, nil, parser.ParseComments)
		if err != nil {
			log.Printf("Skipping file %s due to parsing error: %v\n", file, err)
			continue
		}

		found, err = generate.Generate(generate.Args{Node: fileAst, FS: fs, Builder: &builder, Target: *structType, CopyOnConvert: *copyValues})
		if err != nil {
			if genErr == nil {
				genErr = err
			}
			continue
		}
		if found {
			break
		}
	}

	if !found {
		if genErr != nil {
			log.Fatal(genErr)
		}
		log.Fatalf("Struct %s not found in the provided files", *structType)
	}

	// Generate the output file name dynamically
	outputFileName := fmt.Sprintf("%s_immutable.go", *structType)

	// Write the output to the dynamically named file
	err = os.WriteFile(outputFileName, builder.Bytes(), 0644)
	if err != nil {
		log.Fatalf("Failed to write output file: %v", err)
	}
	log.Printf("Immutable struct generated in %s", outputFileName)
}
