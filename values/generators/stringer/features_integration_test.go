package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommaReplacement tests that the escaped comma replacement works end-to-end
func TestCommaReplacement(t *testing.T) {
	tempDir := t.TempDir()
	stringerBin := buildStringer(t, tempDir)

	// Create a test type file
	testFile := filepath.Join(tempDir, "test.go")
	testCode := `package main

type Punctuation int

const (
	Under_Score Punctuation = iota
	Hyphen_Dash
	Period_Dot
)
`
	if err := os.WriteFile(testFile, []byte(testCode), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Run stringer with escaped comma replacement
	cmd := exec.Command(stringerBin, "-type=Punctuation", "-reverse", `-replace=\,,_`, testFile)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run stringer: %v\n%s", err, output)
	}

	// Read generated file
	generatedFile := filepath.Join(tempDir, "punctuation_string.go")
	content, err := os.ReadFile(generatedFile)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	// Verify the replacement code is present
	if !strings.Contains(string(content), `strings.ReplaceAll(s, ",", "_")`) {
		t.Errorf("Generated code does not contain expected replacement:\n%s", content)
	}

	// Write a test program to verify runtime behavior
	testProgram := filepath.Join(tempDir, "main.go")
	programCode := `package main

import (
	"fmt"
	"os"
)

func main() {
	// Test comma replacement
	val, ok := ReversePunctuation("Under,Score", true)
	if !ok {
		fmt.Println("FAIL: ReversePunctuation(\"Under,Score\", true) returned ok=false")
		os.Exit(1)
	}
	if val != Under_Score {
		fmt.Printf("FAIL: ReversePunctuation(\"Under,Score\", true) = %v, want Under_Score\n", val)
		os.Exit(1)
	}

	// Test another comma replacement
	val, ok = ReversePunctuation("Hyphen,Dash", true)
	if !ok {
		fmt.Println("FAIL: ReversePunctuation(\"Hyphen,Dash\", true) returned ok=false")
		os.Exit(1)
	}
	if val != Hyphen_Dash {
		fmt.Printf("FAIL: ReversePunctuation(\"Hyphen,Dash\", true) = %v, want Hyphen_Dash\n", val)
		os.Exit(1)
	}

	fmt.Println("PASS")
}
`
	if err := os.WriteFile(testProgram, []byte(programCode), 0644); err != nil {
		t.Fatalf("Failed to write test program: %v", err)
	}

	// Run the test program
	cmd = exec.Command("go", "run", testFile, generatedFile, testProgram)
	cmd.Dir = tempDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Test program failed: %v\n%s", err, output)
	}

	if !strings.Contains(string(output), "PASS") {
		t.Errorf("Test program did not pass:\n%s", output)
	}
}

// TestDashReplacement tests that dash-to-underscore replacement works
func TestDashReplacement(t *testing.T) {
	tempDir := t.TempDir()
	stringerBin := buildStringer(t, tempDir)

	// Create a test type file
	testFile := filepath.Join(tempDir, "test.go")
	testCode := `package main

type Status int

const (
	In_Progress Status = iota
	Not_Started
	Completed
)
`
	if err := os.WriteFile(testFile, []byte(testCode), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Run stringer with dash replacement
	cmd := exec.Command(stringerBin, "-type=Status", "-reverse", `-replace=-,_`, testFile)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run stringer: %v\n%s", err, output)
	}

	// Read generated file
	generatedFile := filepath.Join(tempDir, "status_string.go")
	content, err := os.ReadFile(generatedFile)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	// Verify the replacement code is present
	if !strings.Contains(string(content), `strings.ReplaceAll(s, "-", "_")`) {
		t.Errorf("Generated code does not contain expected replacement:\n%s", content)
	}

	// Write a test program to verify runtime behavior
	testProgram := filepath.Join(tempDir, "main.go")
	programCode := `package main

import (
	"fmt"
	"os"
)

func main() {
	// Test dash replacement
	val, ok := ReverseStatus("In-Progress", true)
	if !ok {
		fmt.Println("FAIL: ReverseStatus(\"In-Progress\", true) returned ok=false")
		os.Exit(1)
	}
	if val != In_Progress {
		fmt.Printf("FAIL: ReverseStatus(\"In-Progress\", true) = %v, want In_Progress\n", val)
		os.Exit(1)
	}

	// Test case insensitive with dash replacement
	val, ok = ReverseStatus("not-started", false)
	if !ok {
		fmt.Println("FAIL: ReverseStatus(\"not-started\", false) returned ok=false")
		os.Exit(1)
	}
	if val != Not_Started {
		fmt.Printf("FAIL: ReverseStatus(\"not-started\", false) = %v, want Not_Started\n", val)
		os.Exit(1)
	}

	fmt.Println("PASS")
}
`
	if err := os.WriteFile(testProgram, []byte(programCode), 0644); err != nil {
		t.Fatalf("Failed to write test program: %v", err)
	}

	// Run the test program
	cmd = exec.Command("go", "run", testFile, generatedFile, testProgram)
	cmd.Dir = tempDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Test program failed: %v\n%s", err, output)
	}

	if !strings.Contains(string(output), "PASS") {
		t.Errorf("Test program did not pass:\n%s", output)
	}
}

// TestJSONMarshaling tests that the -marshal flag generates working JSON methods
func TestJSONMarshaling(t *testing.T) {
	tempDir := t.TempDir()
	stringerBin := buildStringer(t, tempDir)

	// Create a test type file
	testFile := filepath.Join(tempDir, "test.go")
	testCode := `package main

type Color int

const (
	Red Color = iota
	Green
	Blue
)
`
	if err := os.WriteFile(testFile, []byte(testCode), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Run stringer with marshal and reverse
	cmd := exec.Command(stringerBin, "-type=Color", "-reverse", "-marshal", testFile)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run stringer: %v\n%s", err, output)
	}

	// Read generated file
	generatedFile := filepath.Join(tempDir, "color_string.go")
	content, err := os.ReadFile(generatedFile)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	// Verify the JSON methods are present
	if !strings.Contains(string(content), "func (i Color) MarshalJSON()") {
		t.Errorf("Generated code does not contain MarshalJSON method:\n%s", content)
	}
	if !strings.Contains(string(content), "func (i *Color) UnmarshalJSON(") {
		t.Errorf("Generated code does not contain UnmarshalJSON method:\n%s", content)
	}
	// Verify unsafe.String is used
	if !strings.Contains(string(content), "unsafe.String(") {
		t.Errorf("Generated code does not use unsafe.String:\n%s", content)
	}
	// Verify Valid() method is generated (marshal auto-enables -valid)
	if !strings.Contains(string(content), "func (i Color) Valid() bool") {
		t.Errorf("Generated code does not contain Valid() method:\n%s", content)
	}
	// Verify MarshalJSON checks Valid()
	if !strings.Contains(string(content), "if !i.Valid()") {
		t.Errorf("Generated code does not check Valid() in MarshalJSON:\n%s", content)
	}

	// Write a test program to verify runtime behavior
	testProgram := filepath.Join(tempDir, "main.go")
	programCode := `package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	// Test MarshalJSON
	c := Red
	data, err := json.Marshal(c)
	if err != nil {
		fmt.Printf("FAIL: json.Marshal(Red) returned error: %v\n", err)
		os.Exit(1)
	}
	if string(data) != "\"Red\"" {
		fmt.Printf("FAIL: json.Marshal(Red) = %s, want \"Red\"\n", string(data))
		os.Exit(1)
	}

	// Test UnmarshalJSON
	var c2 Color
	err = json.Unmarshal([]byte("\"Green\""), &c2)
	if err != nil {
		fmt.Printf("FAIL: json.Unmarshal(\"Green\") returned error: %v\n", err)
		os.Exit(1)
	}
	if c2 != Green {
		fmt.Printf("FAIL: json.Unmarshal(\"Green\") = %v, want Green\n", c2)
		os.Exit(1)
	}

	// Test invalid JSON
	var c3 Color
	err = json.Unmarshal([]byte("\"InvalidColor\""), &c3)
	if err == nil {
		fmt.Println("FAIL: json.Unmarshal(\"InvalidColor\") should return error")
		os.Exit(1)
	}

	// Test marshaling invalid value
	invalidColor := Color(100)
	_, err = json.Marshal(invalidColor)
	if err == nil {
		fmt.Println("FAIL: json.Marshal(Color(100)) should return error for invalid value")
		os.Exit(1)
	}

	fmt.Println("PASS")
}
`
	if err := os.WriteFile(testProgram, []byte(programCode), 0644); err != nil {
		t.Fatalf("Failed to write test program: %v", err)
	}

	// Run the test program
	cmd = exec.Command("go", "run", testFile, generatedFile, testProgram)
	cmd.Dir = tempDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Test program failed: %v\n%s", err, output)
	}

	if !strings.Contains(string(output), "PASS") {
		t.Errorf("Test program did not pass:\n%s", output)
	}
}

// TestMarshalInsensitive tests that the -marshalinsensitive flag makes UnmarshalJSON case-insensitive
func TestMarshalInsensitive(t *testing.T) {
	tempDir := t.TempDir()
	stringerBin := buildStringer(t, tempDir)

	// Create a test type file
	testFile := filepath.Join(tempDir, "test.go")
	testCode := `package main

type Color int

const (
	Red Color = iota
	Green
	Blue
)
`
	if err := os.WriteFile(testFile, []byte(testCode), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Run stringer with marshalinsensitive (which should auto-enable marshal and reverse)
	cmd := exec.Command(stringerBin, "-type=Color", "-marshalinsensitive", testFile)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run stringer: %v\n%s", err, output)
	}

	// Read generated file
	generatedFile := filepath.Join(tempDir, "color_string.go")
	content, err := os.ReadFile(generatedFile)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	// Verify the JSON methods are present
	if !strings.Contains(string(content), "func (i Color) MarshalJSON()") {
		t.Errorf("Generated code does not contain MarshalJSON method:\n%s", content)
	}
	if !strings.Contains(string(content), "func (i *Color) UnmarshalJSON(") {
		t.Errorf("Generated code does not contain UnmarshalJSON method:\n%s", content)
	}
	// Verify Reverse function is called with false (case insensitive)
	if !strings.Contains(string(content), "ReverseColor(s, false)") {
		t.Errorf("Generated code does not call ReverseColor with false (case insensitive):\n%s", content)
	}

	// Write a test program to verify runtime behavior
	testProgram := filepath.Join(tempDir, "main.go")
	programCode := `package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	// Test case-insensitive unmarshal - lowercase
	var c1 Color
	err := json.Unmarshal([]byte("\"red\""), &c1)
	if err != nil {
		fmt.Printf("FAIL: json.Unmarshal(\"red\") returned error: %v\n", err)
		os.Exit(1)
	}
	if c1 != Red {
		fmt.Printf("FAIL: json.Unmarshal(\"red\") = %v, want Red\n", c1)
		os.Exit(1)
	}

	// Test case-insensitive unmarshal - mixed case
	var c2 Color
	err = json.Unmarshal([]byte("\"GrEeN\""), &c2)
	if err != nil {
		fmt.Printf("FAIL: json.Unmarshal(\"GrEeN\") returned error: %v\n", err)
		os.Exit(1)
	}
	if c2 != Green {
		fmt.Printf("FAIL: json.Unmarshal(\"GrEeN\") = %v, want Green\n", c2)
		os.Exit(1)
	}

	// Test case-insensitive unmarshal - uppercase
	var c3 Color
	err = json.Unmarshal([]byte("\"BLUE\""), &c3)
	if err != nil {
		fmt.Printf("FAIL: json.Unmarshal(\"BLUE\") returned error: %v\n", err)
		os.Exit(1)
	}
	if c3 != Blue {
		fmt.Printf("FAIL: json.Unmarshal(\"BLUE\") = %v, want Blue\n", c3)
		os.Exit(1)
	}

	fmt.Println("PASS")
}
`
	if err := os.WriteFile(testProgram, []byte(programCode), 0644); err != nil {
		t.Fatalf("Failed to write test program: %v", err)
	}

	// Run the test program
	cmd = exec.Command("go", "run", testFile, generatedFile, testProgram)
	cmd.Dir = tempDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Test program failed: %v\n%s", err, output)
	}

	if !strings.Contains(string(output), "PASS") {
		t.Errorf("Test program did not pass:\n%s", output)
	}
}

func TestParseReplacementPair(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedOld string
		expectedNew string
		wantErr     bool
	}{
		{
			name:        "Success: Simple replacement",
			input:       "-,_",
			expectedOld: "-",
			expectedNew: "_",
			wantErr:     false,
		},
		{
			name:        "Success: Escaped comma in old",
			input:       `\,,_`,
			expectedOld: ",",
			expectedNew: "_",
			wantErr:     false,
		},
		{
			name:        "Success: Escaped comma in new",
			input:       `_,\,`,
			expectedOld: "_",
			expectedNew: ",",
			wantErr:     false,
		},
		{
			name:        "Success: Escaped backslash",
			input:       `\\,/`,
			expectedOld: `\`,
			expectedNew: "/",
			wantErr:     false,
		},
		{
			name:        "Success: Multiple escapes",
			input:       `a\,b\,c,x\,y`,
			expectedOld: "a,b,c",
			expectedNew: "x,y",
			wantErr:     false,
		},
		{
			name:        "Success: Escaped backslash before comma",
			input:       `a\\,b`,
			expectedOld: `a\`,
			expectedNew: "b",
			wantErr:     false,
		},
		{
			name:        "Success: Forward compatibility - unknown escape",
			input:       `\n,\t`,
			expectedOld: "n",
			expectedNew: "t",
			wantErr:     false,
		},
		{
			name:        "Success: Empty strings",
			input:       ",",
			expectedOld: "",
			expectedNew: "",
			wantErr:     false,
		},
		{
			name:        "Success: Empty old value",
			input:       ",new",
			expectedOld: "",
			expectedNew: "new",
			wantErr:     false,
		},
		{
			name:        "Success: Empty new value",
			input:       "old,",
			expectedOld: "old",
			expectedNew: "",
			wantErr:     false,
		},
		{
			name:        "Success: Escaped comma - no split",
			input:       `a\,b,c`,
			expectedOld: "a,b",
			expectedNew: "c",
			wantErr:     false,
		},
		{
			name:        "Success: Trailing backslash in new",
			input:       `a,b\`,
			expectedOld: "a",
			expectedNew: `b\`,
			wantErr:     false,
		},
		{
			name:    "Error: No comma",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "Error: Multiple unescaped commas",
			input:   "a,b,c",
			wantErr: true,
		},
		{
			name:    "Error: Empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:        "Success: Complex example with multiple escapes",
			input:       `func\,\(int\,,func\)`,
			expectedOld: `func,(int,`,
			expectedNew: `func)`,
			wantErr:     false,
		},
		{
			name:        "Success: Both parts have escaped commas and backslashes",
			input:       `a\,b\\c,x\,y\\z`,
			expectedOld: `a,b\c`,
			expectedNew: `x,y\z`,
			wantErr:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			old, new, err := parseReplacementPair(test.input)

			switch {
			case err == nil && test.wantErr:
				t.Errorf("[TestParseReplacementPair(%s)]: got err == nil, want err != nil", test.name)
				return
			case err != nil && !test.wantErr:
				t.Errorf("[TestParseReplacementPair(%s)]: got err == %s, want err == nil", test.name, err)
				return
			case err != nil:
				return
			}

			if old != test.expectedOld {
				t.Errorf("[TestParseReplacementPair(%s)]: old: got %q, want %q", test.name, old, test.expectedOld)
			}
			if new != test.expectedNew {
				t.Errorf("[TestParseReplacementPair(%s)]: new: got %q, want %q", test.name, new, test.expectedNew)
			}
		})
	}
}

func TestGeneratorParseReplacements(t *testing.T) {
	tests := []struct {
		name          string
		replaceFlags  []string
		expectedPairs []ReplacePair
		wantErr       bool
	}{
		{
			name:         "Success: Single replacement",
			replaceFlags: []string{"-,_"},
			expectedPairs: []ReplacePair{
				{old: "-", new: "_"},
			},
			wantErr: false,
		},
		{
			name:         "Success: Multiple replacements",
			replaceFlags: []string{"-,_", " ,_"},
			expectedPairs: []ReplacePair{
				{old: "-", new: "_"},
				{old: "", new: "_"}, // Space gets trimmed
			},
			wantErr: false,
		},
		{
			name:         "Success: Escaped commas",
			replaceFlags: []string{`\,,_`, `_,\,`},
			expectedPairs: []ReplacePair{
				{old: ",", new: "_"},
				{old: "_", new: ","},
			},
			wantErr: false,
		},
		{
			name:         "Success: With whitespace trimming",
			replaceFlags: []string{" - , _ "},
			expectedPairs: []ReplacePair{
				{old: "-", new: "_"},
			},
			wantErr: false,
		},
		{
			name:          "Success: Empty list",
			replaceFlags:  []string{},
			expectedPairs: nil,
			wantErr:       false,
		},
		{
			name:         "Error: Invalid format",
			replaceFlags: []string{"invalid"},
			wantErr:      true,
		},
		{
			name:         "Error: Too many parts",
			replaceFlags: []string{"a,b,c"},
			wantErr:      true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := &Generator{}
			err := g.parseReplacements(test.replaceFlags)

			switch {
			case err == nil && test.wantErr:
				t.Errorf("[TestGeneratorParseReplacements(%s)]: got err == nil, want err != nil", test.name)
				return
			case err != nil && !test.wantErr:
				t.Errorf("[TestGeneratorParseReplacements(%s)]: got err == %s, want err == nil", test.name, err)
				return
			case err != nil:
				return
			}

			if len(g.replacements) != len(test.expectedPairs) {
				t.Errorf("[TestGeneratorParseReplacements(%s)]: got %d replacements, want %d", test.name, len(g.replacements), len(test.expectedPairs))
				return
			}

			for i, expected := range test.expectedPairs {
				if g.replacements[i].old != expected.old {
					t.Errorf("[TestGeneratorParseReplacements(%s)]: replacement[%d].old: got %q, want %q", test.name, i, g.replacements[i].old, expected.old)
				}
				if g.replacements[i].new != expected.new {
					t.Errorf("[TestGeneratorParseReplacements(%s)]: replacement[%d].new: got %q, want %q", test.name, i, g.replacements[i].new, expected.new)
				}
			}
		})
	}
}

// TestListFunction tests that the -list flag generates a working List function
func TestListFunction(t *testing.T) {
	tempDir := t.TempDir()
	stringerBin := buildStringer(t, tempDir)

	// Create a test type file
	testFile := filepath.Join(tempDir, "test.go")
	testCode := `package main

type Fruit int

const (
	Apple Fruit = iota
	Orange
	Banana
)
`
	if err := os.WriteFile(testFile, []byte(testCode), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Run stringer with -list
	cmd := exec.Command(stringerBin, "-type=Fruit", "-list", testFile)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run stringer: %v\n%s", err, output)
	}

	// Read generated file
	generatedFile := filepath.Join(tempDir, "fruit_string.go")
	content, err := os.ReadFile(generatedFile)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	// Verify the List function is present
	if !strings.Contains(string(content), "func ListFruit() iter.Seq[Fruit]") {
		t.Errorf("Generated code does not contain ListFruit function:\n%s", content)
	}

	// Write a test program to verify runtime behavior
	testProgram := filepath.Join(tempDir, "main.go")
	programCode := `package main

import (
	"fmt"
	"os"
)

func main() {
	expected := []Fruit{Apple, Orange, Banana}
	i := 0
	for v := range ListFruit() {
		if i >= len(expected) {
			fmt.Printf("FAIL: ListFruit yielded more values than expected\n")
			os.Exit(1)
		}
		if v != expected[i] {
			fmt.Printf("FAIL: ListFruit[%d] = %v, want %v\n", i, v, expected[i])
			os.Exit(1)
		}
		i++
	}
	if i != len(expected) {
		fmt.Printf("FAIL: ListFruit yielded %d values, want %d\n", i, len(expected))
		os.Exit(1)
	}

	// Test early break
	count := 0
	for range ListFruit() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		fmt.Printf("FAIL: early break: got %d iterations, want 2\n", count)
		os.Exit(1)
	}

	fmt.Println("PASS")
}
`
	if err := os.WriteFile(testProgram, []byte(programCode), 0644); err != nil {
		t.Fatalf("Failed to write test program: %v", err)
	}

	// Run the test program
	cmd = exec.Command("go", "run", testFile, generatedFile, testProgram)
	cmd.Dir = tempDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Test program failed: %v\n%s", err, output)
	}

	if !strings.Contains(string(output), "PASS") {
		t.Errorf("Test program did not pass:\n%s", output)
	}
}

// buildStringer builds the stringer binary for testing
func buildStringer(t *testing.T, tempDir string) string {
	stringerBin := filepath.Join(tempDir, "stringer")
	cmd := exec.Command("go", "build", "-o", stringerBin, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build stringer: %v\n%s", err, output)
	}
	return stringerBin
}
