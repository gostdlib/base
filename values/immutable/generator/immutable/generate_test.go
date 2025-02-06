package immutable

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	mutatedMethod = "testdata/data/bad/mutatedMethod"
	sharedName    = "testdata/data/bad/private_public_share_name"
	goodData      = "testdata/data/good"
)

func TestGenerate(t *testing.T) {
	tests := []struct {
		name       string
		pkgLoc     string
		structName string
		wantErr    bool
		wantFound  bool
	}{
		{
			name:       "Error: bad type, mutates data in method",
			structName: "Generic",
			pkgLoc:     mutatedMethod,
			wantFound:  false,
			wantErr:    true,
		},
		{
			name:       "Error: bad type, public and private fields of the same name",
			structName: "Generic",
			pkgLoc:     sharedName,
			wantFound:  false,
			wantErr:    true,
		},
		{
			name:       "Good type",
			structName: "Generic",
			pkgLoc:     goodData,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "Good type",
			structName: "GenericOneType",
			pkgLoc:     goodData,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "Good type",
			structName: "NonGeneric",
			pkgLoc:     goodData,
			wantFound:  true,
			wantErr:    false,
		},
	}

	for _, test := range tests {
		goFiles, err := filepath.Glob(filepath.Join(test.pkgLoc, "*.go"))
		if err != nil {
			panic(err)
		}

		var builder bytes.Buffer

		// Process each Go file until the target struct is found
		found := false
		for _, file := range goFiles {
			if strings.HasSuffix(file, ImmutableSuffix) || strings.HasSuffix(file, ImmutableTestSuffix) {
				continue
			}

			fs := token.NewFileSet()
			fileAst, err := parser.ParseFile(fs, file, nil, parser.ParseComments)
			if err != nil {
				log.Printf("Skipping file %s due to parsing error: %v\n", file, err)
				continue
			}

			found, err = Generate(fileAst, fs, &builder, test.structName)
			if err != nil {
				if !test.wantErr {
					t.Fatalf("TestGenerate: got err == %s, want err == nil", err)
				}
				break
			}
			if found {
				break
			}
		}

		if found != test.wantFound {
			t.Fatalf("TestGenerate: got found == %t, want found == %t", found, test.wantFound)
		}

		if !found {
			continue
		}

		abs, err := filepath.Abs(test.pkgLoc)
		if err != nil {
			panic(err)
		}
		// Generate the output file name dynamically
		outputFileName := fmt.Sprintf("%s_immutable.go", test.structName)
		err = os.WriteFile(
			filepath.Join(abs, outputFileName),
			builder.Bytes(),
			0644,
		)
		if err != nil {
			panic(err)
		}

		goexec, err := exec.LookPath("go")
		if err != nil {
			panic(fmt.Sprintf("cannot find go on path: %s", err))
		}

		cmd := exec.Cmd{
			Path: goexec,
			Dir:  abs,
			Args: []string{"go", "build"},
			Env:  os.Environ(),
		}

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("TestGenerate(go build): got err == %s, want err == nil, output from command:\n%s", err, string(out))
		}
	}
}
