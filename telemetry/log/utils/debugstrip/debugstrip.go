//go:build unix

// Package debugstrip provides a utility to reformat JSON log lines into a more human-readable format.
// This is useful when not using a structured log viewer, such as when you want to quickly scan logs in a terminal.
// It reads from standard input and writes to standard output. Lines that are not valid JSON or do not
// contain the expected fields are passed through unchanged.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unsafe"

	"github.com/go-json-experiment/json"
	"github.com/gostdlib/base/concurrency/sync"
)

func main() {
	scan(context.Background(), os.Stdin, os.Stdout)
}

var lineReturn = []byte{'\n'}

// scan reads lines from the input reader, reformats them if they are JSON log lines, and writes them to the output writer.
func scan(ctx context.Context, in io.Reader, out io.Writer) {
	// Create a new scanner to read from os.Stdin
	scanner := bufio.NewScanner(in)

	for {
		// Scan for the next token, which by default is a line
		if scanner.Scan() {
			// Get the text of the scanned line
			line := scanner.Bytes()
			_, err := out.Write(reframe(ctx, line))
			if err != nil {
				return
			}
			_, _ = out.Write(lineReturn) // Ignore this error.
			continue
		}
		return
	}
}

var reqKeys = []string{
	"time",
	"level",
	"msg",
}

// mapHolder holds a map that we can then look through to see if we have the keys
// required to be one of our log messages before reformatting.
type mapHolder struct {
	m map[string]any
}

func (m mapHolder) valid() bool {
	for _, k := range reqKeys {
		if _, ok := m.m[k]; !ok {
			return false
		}
	}
	// Check for file and line either at top level or in source object
	hasFile := false
	hasLine := false

	if _, ok := m.m["file"]; ok {
		hasFile = true
	}
	if _, ok := m.m["line"]; ok {
		hasLine = true
	}

	if source, ok := m.m["source"].(map[string]any); ok {
		if _, ok := source["file"]; ok {
			hasFile = true
		}
		if _, ok := source["line"]; ok {
			hasLine = true
		}
	}

	return hasFile && hasLine
}

func (m mapHolder) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.m)
}

func (m mapHolder) Reset() {
	clear(m.m)
}

func (m mapHolder) level() string {
	v, ok := m.m["level"].(string)
	if !ok {
		return "UnknownLevel"
	}
	return strings.ToUpper(v)
}

func (m mapHolder) line() int {
	// Try top level first
	switch v := m.m["line"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}

	// Try source object
	if source, ok := m.m["source"].(map[string]any); ok {
		switch v := source["line"].(type) {
		case int:
			return v
		case float64:
			return int(v)
		}
	}

	return 0
}

func (m mapHolder) shortFile() string {
	// Try top level first
	v, ok := m.m["file"].(string)
	if !ok {
		// Try source object
		if source, ok := m.m["source"].(map[string]any); ok {
			v, ok = source["file"].(string)
			if !ok {
				return "unknown"
			}
		} else {
			return "unknown"
		}
	}
	parts := strings.Split(v, "/")
	if len(parts) == 0 {
		return v
	}
	return parts[len(parts)-1]
}

func (m mapHolder) hourMinuteSecond() string {
	v, ok := m.m["time"].(string)
	if !ok {
		return "00:00:00"
	}
	tm, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return "00:00:00"
	}
	return tm.Format(`15:04:05`)
}

func (m mapHolder) msg() string {
	s, ok := m.m["msg"].(string)
	if !ok {
		return "no message"
	}
	return s
}

var pool = sync.NewPool(
	context.Background(),
	"mapHolder",
	func() mapHolder {
		return mapHolder{m: make(map[string]any)}
	},
	sync.WithBuffer(10),
)

// reframe looks for a JSON log line that it understands and reformats it to a shorter form.
func reframe(ctx context.Context, line []byte) []byte {
	holder := pool.Get(ctx)
	defer pool.Put(ctx, holder)

	if err := json.Unmarshal(line, &holder.m); err != nil {
		return line
	}

	if !holder.valid() {
		return line
	}

	// Original line:
	//{"time":"2025-09-09T18:23:06.102045-07:00","level":"ERROR","source":{"function":"path/to/package.(*Type).read","file":"/file/on/the/filesystem/it/was/created/on/checker.go","line":110},"msg":"failed to read configuration file: Internal error occurred: test error"}

	// Reformatted line:
	//[INFO][18:23:06][.../checker.go:110]: failed to read configuration file: Internal error occurred: test error
	return strToBytes(fmt.Sprintf("[%s][%s][.../%s:%d]: %s", holder.level(), holder.hourMinuteSecond(), holder.shortFile(), holder.line(), holder.msg()))
}

func strToBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
