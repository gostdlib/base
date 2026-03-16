package main

import (
	"bytes"
	"strings"
	"testing"
	"text/template"
)

func TestInitTemplate(t *testing.T) {
	t.Parallel()

	tmpl, err := template.New("init").Parse(initTmpl)
	if err != nil {
		t.Fatalf("TestInitTemplate: failed to parse template: %s", err)
	}

	args := tmplArgs{
		PkgName: "myservice",
		Initer:  "Init",
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, args); err != nil {
		t.Fatalf("TestInitTemplate: failed to execute template: %s", err)
	}

	output := buf.String()

	if !strings.HasPrefix(output, "package myservice") {
		t.Errorf("TestInitTemplate: output does not start with 'package myservice', got prefix: %q", output[:min(50, len(output))])
	}

	if strings.Contains(output, "gointt") {
		t.Errorf("TestInitTemplate: output contains typo 'gointt', want 'goinit'")
	}

	if strings.Contains(output, "return goinit.Service") {
		t.Errorf("TestInitTemplate: output contains 'return goinit.Service' in void function")
	}

	if strings.Contains(output, "return goinit.Close") {
		t.Errorf("TestInitTemplate: output contains 'return goinit.Close' in void function")
	}
}
