package generate

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
	mutatedMethod   = "testdata/data/bad/mutatedMethod"
	mutatedNonR     = "testdata/data/bad/mutatedNonRReceiver"
	incDecMutation  = "testdata/data/bad/incDecMutation"
	sharedName      = "testdata/data/bad/private_public_share_name"
	goodData        = "testdata/data/good"
	unnamedReceiver = "testdata/data/unnamedReceiver"
	valueRecvMulti  = "testdata/data/valueRecvMultiParam"
	multiLine       = "testdata/data/multiLineComment"
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
			name:       "Error: bad type, mutates data in method via non-r receiver",
			structName: "Generic",
			pkgLoc:     mutatedNonR,
			wantFound:  false,
			wantErr:    true,
		},
		{
			name:       "Error: bad type, mutates data in method via increment operator",
			structName: "Generic",
			pkgLoc:     incDecMutation,
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
			name:       "Success: generic struct with two type params",
			structName: "Generic",
			pkgLoc:     goodData,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "Success: generic struct with one type param",
			structName: "GenericOneType",
			pkgLoc:     goodData,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "Success: non-generic struct",
			structName: "NonGeneric",
			pkgLoc:     goodData,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "Success: scalar-only struct must not import immutable package",
			structName: "ScalarOnly",
			pkgLoc:     goodData,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "Success: method with unnamed receiver is skipped without panicking",
			structName: "Unnamed",
			pkgLoc:     unnamedReceiver,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "Success: value receiver with multiple type params is copied so its import is consumed",
			structName: "Pair",
			pkgLoc:     valueRecvMulti,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "Success: multi-line struct and field comments stay valid Go",
			structName: "MultiLine",
			pkgLoc:     multiLine,
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
		var genErr error
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
				genErr = err
				break
			}
			if found {
				break
			}
		}

		switch {
		case genErr == nil && test.wantErr:
			t.Errorf("TestGenerate(%s): got err == nil, want err != nil", test.name)
			continue
		case genErr != nil && !test.wantErr:
			t.Errorf("TestGenerate(%s): got err == %s, want err == nil", test.name, genErr)
			continue
		case genErr != nil:
			continue
		}

		if found != test.wantFound {
			t.Errorf("TestGenerate(%s): got found == %t, want found == %t", test.name, found, test.wantFound)
			continue
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
			t.Errorf("TestGenerate(%s): go build: got err == %s, want err == nil, output:\n%s", test.name, err, string(out))
		}
	}
}
