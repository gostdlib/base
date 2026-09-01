// Package debugstrip provides a utility to reformat JSON log lines into a more human-readable format.
// This is useful when not using a structured log viewer, such as when you want to quickly scan logs in a terminal.
// It reads from standard input and writes to standard output. Lines that are not valid JSON or do not
// contain the expected fields are passed through unchanged.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unsafe"

	"github.com/go-json-experiment/json"
	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/values/sizes"
)

func main() {
	if err := scan(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "debugstrip: %s\n", err)
		os.Exit(1)
	}
}

var (
	lineReturn     = []byte{'\n'}
	carriageReturn = []byte{'\r'}
)

// maxLine is the longest log line scan will read. A bufio.Scanner's 64 KiB default is far too small: a line carrying
// a stack trace or a dumped object routinely runs past it, and those are precisely the lines this tool exists to
// read. The limit is here only so that input with no newline at all cannot grow one allocation without bound.
const maxLine = 64 * sizes.MiB

// scan reads lines from the input reader, reformats them if they are JSON log lines, and writes them to the output writer.
func scan(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	buf := &bytes.Buffer{}

	for {
		line, readErr := readLine(reader, buf, maxLine)
		if len(line) > 0 {
			if _, err := out.Write(reframe(ctx, trimEOL(line))); err != nil {
				return fmt.Errorf("writing output: %w", err)
			}
			if _, err := out.Write(lineReturn); err != nil {
				return fmt.Errorf("writing output: %w", err)
			}
		}
		switch {
		case errors.Is(readErr, io.EOF):
			return nil
		case readErr != nil:
			return fmt.Errorf("reading input: %w", readErr)
		}
	}
}

// readLine reads one line into buf and returns it, refusing to grow past limit bytes. bufio.Reader.ReadBytes cannot
// be used for this: it has no limit of its own, so a newline-free stream would buffer entirely into memory. The limit
// is a parameter rather than maxLine itself so the refusal can be tested without building a maxLine-sized input. The
// returned slice is owned by buf and is only valid until the next call.
func readLine(reader *bufio.Reader, buf *bytes.Buffer, limit int) ([]byte, error) {
	buf.Reset()
	for {
		// ReadSlice returns what it has with ErrBufferFull when its buffer fills before the delimiter, so the
		// line is accumulated a chunk at a time. Its slice points into the reader, so it must be copied now.
		chunk, err := reader.ReadSlice('\n')
		if buf.Len()+len(chunk) > limit {
			return nil, fmt.Errorf("a single line ran past the %d byte limit", limit)
		}
		buf.Write(chunk)
		if err == bufio.ErrBufferFull {
			continue
		}
		return buf.Bytes(), err
	}
}

// trimEOL removes the line terminator that readLine leaves on the line, including a CRLF's carriage return.
func trimEOL(line []byte) []byte {
	line = bytes.TrimSuffix(line, lineReturn)
	return bytes.TrimSuffix(line, carriageReturn)
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

func (m *mapHolder) valid() bool {
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

func (m *mapHolder) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.m)
}

func (m *mapHolder) Reset() {
	clear(m.m)
}

func (m *mapHolder) level() string {
	v, ok := m.m["level"].(string)
	if !ok {
		return "UnknownLevel"
	}
	return strings.ToUpper(v)
}

func (m *mapHolder) line() int {
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

func (m *mapHolder) shortFile() string {
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

func (m *mapHolder) hourMinuteSecond() string {
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

func (m *mapHolder) msg() string {
	s, ok := m.m["msg"].(string)
	if !ok {
		return "no message"
	}
	return s
}

var pool = sync.NewPool(
	context.Background(),
	"mapHolder",
	func() *mapHolder {
		return &mapHolder{m: make(map[string]any)}
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
