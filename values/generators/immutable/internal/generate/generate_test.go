package generate

import (
	"bytes"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

// update rewrites the golden files from the generator's current output instead of comparing against them. Run
// "go test ./values/generators/immutable/internal/generate/ -update" after an intended change to the generator, then
// read the diff before committing it. The package has to be named: a wider pattern passes -update to test binaries
// that never declared it.
var update = flag.Bool("update", false, "rewrite the testdata golden files instead of comparing against them")

const (
	mutatedMethod   = "testdata/data/bad/mutatedMethod"
	mutatedNonR     = "testdata/data/bad/mutatedNonRReceiver"
	incDecMutation  = "testdata/data/bad/incDecMutation"
	sharedName      = "testdata/data/bad/private_public_share_name"
	goodData        = "testdata/data/good"
	unnamedReceiver = "testdata/data/unnamedReceiver"
	valueRecvMulti  = "testdata/data/valueRecvMultiParam"
	multiLine       = "testdata/data/multiLineComment"
	preDeclared     = "testdata/data/preDeclaredImmutable"
	copyOnConvert   = "testdata/data/copyOnConvert"
	localTypeShadow = "testdata/data/localTypeShadow"
	collision       = "testdata/data/immutableCollision"
	unrelatedImm    = "testdata/data/unrelatedImmutable"
	foreignField    = "testdata/data/foreignImmutableField"
	aliasedColl     = "testdata/data/aliasedCollision"
)

// sourceLines splits generated source for pretty.Compare, which renders a whole file as one escaped string but a
// slice of lines as a readable per-line diff.
func sourceLines(s string) []string {
	return strings.Split(s, "\n")
}

func TestPathName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "Success: a plain path names its last element",
			in:   "example.com/foo/bar",
			want: "bar",
		},
		{
			name: "Success: a major version suffix is skipped",
			in:   "example.com/foo/immutable/v2",
			want: "immutable",
		},
		{
			name: "Success: a v-prefixed package that is not a version keeps its name",
			in:   "example.com/foo/vector",
			want: "vector",
		},
		{
			name: "Success: a single element path is its own name",
			in:   "immutable",
			want: "immutable",
		},
	}

	for _, test := range tests {
		if got := pathName(test.in); got != test.want {
			t.Errorf("TestPathName(%s): got %q, want %q", test.name, got, test.want)
		}
	}
}

func TestDocComment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "Success: a single line",
			in:   "Name is the name.\n",
			want: "// Name is the name.",
		},
		{
			name: "Success: every line after the first gets its own marker",
			in:   "Name is the name.\nIt spans two lines.\n",
			want: "// Name is the name.\n// It spans two lines.",
		},
		{
			name: "Success: a blank line becomes a bare marker",
			in:   "Name is the name.\n\nMore about it.\n",
			want: "// Name is the name.\n//\n// More about it.",
		},
		{
			name: "Success: an indented code block keeps its indentation",
			in:   "Example:\n\n\tx := 1\n\tuse(x)\n\nDone.\n",
			want: "// Example:\n//\n//\tx := 1\n//\tuse(x)\n//\n// Done.",
		},
		{
			name: "Success: a space indented code block keeps its indentation",
			in:   "Example:\n\n    x := 1\n",
			want: "// Example:\n//\n//    x := 1",
		},
		{
			name: "Success: trailing whitespace is dropped",
			in:   "Name is the name.   \n",
			want: "// Name is the name.",
		},
		{
			name: "Success: leading and trailing blank lines are dropped",
			in:   "\n\nName is the name.\n\n\n",
			want: "// Name is the name.",
		},
	}

	for _, test := range tests {
		got := docComment(test.in)
		if got != test.want {
			t.Errorf("TestDocComment(%s): got %q, want %q", test.name, got, test.want)
		}
	}
}

func TestGenerate(t *testing.T) {
	tests := []struct {
		name       string
		pkgLoc     string
		structName string
		copyValues bool // Generate with -copy, so Immutable() copies maps and slices instead of sharing them.
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
			name:       "Error: a different package is already imported as immutable",
			structName: "Collides",
			pkgLoc:     collision,
			wantFound:  false,
			wantErr:    true,
		},
		{
			name:       "Error: a foreign package aliased as immutable is used by the target",
			structName: "Aliased",
			pkgLoc:     aliasedColl,
			wantFound:  false,
			wantErr:    true,
		},
		{
			name:       "Success: a foreign immutable import used only by another type is no collision",
			structName: "Sidecar",
			pkgLoc:     unrelatedImm,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "Success: a target holding only a foreign immutable field emits no import of our own",
			structName: "Passthrough",
			pkgLoc:     foreignField,
			wantFound:  true,
			wantErr:    false,
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
		{
			name:       "Success: a pre-declared immutable field and a leading code block",
			structName: "PreDeclared",
			pkgLoc:     preDeclared,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "Success: a function-local type with the target's name is skipped",
			structName: "Shadowed",
			pkgLoc:     localTypeShadow,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "Success: -copy makes Immutable copy its maps and slices",
			structName: "Copied",
			pkgLoc:     copyOnConvert,
			copyValues: true,
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
			if SkipFile(file) {
				continue
			}

			fs := token.NewFileSet()
			fileAst, err := parser.ParseFile(fs, file, nil, parser.ParseComments)
			if err != nil {
				log.Printf("Skipping file %s due to parsing error: %v\n", file, err)
				continue
			}

			found, err = Generate(Args{Node: fileAst, FS: fs, Builder: &builder, Target: test.structName, CopyOnConvert: test.copyValues})
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
		golden := filepath.Join(abs, test.structName+ImmutableSuffix)

		if *update {
			if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
				panic(err)
			}
			if err := os.WriteFile(golden, builder.Bytes(), 0644); err != nil {
				panic(err)
			}
		}

		want, err := os.ReadFile(golden)
		if err != nil {
			t.Errorf("TestGenerate(%s): reading golden file %s: got err == %s, want err == nil; re-run with -update to create it", test.name, filepath.Base(golden), err)
			continue
		}
		// The golden file holds the exact source the generator is expected to produce, so a change in what it
		// emits has to be looked at rather than absorbed. Building alone does not catch it: a doc comment can
		// lose its code block indentation, or a comment can move, and the file still compiles.
		if diff := pretty.Compare(sourceLines(string(want)), sourceLines(builder.String())); diff != "" {
			t.Errorf("TestGenerate(%s): generated output differs from golden file %s, -want/+got:\n%s\nIf the change is intended, re-run with -update", test.name, filepath.Base(golden), diff)
			continue
		}

		// The golden file on disk is byte-identical to what was just generated, so building the package proves the
		// generated source compiles.
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
