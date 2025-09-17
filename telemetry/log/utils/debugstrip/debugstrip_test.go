package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

func TestMapHolderValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    map[string]any
		want bool
	}{
		{
			name: "Success: all required keys present",
			m: map[string]any{
				"time":  "2025-09-09T18:23:06.102045-07:00",
				"level": "ERROR",
				"file":  "/path/to/file.go",
				"line":  110,
				"msg":   "test message",
			},
			want: true,
		},
		{
			name: "Error: missing time key",
			m: map[string]any{
				"level": "ERROR",
				"file":  "/path/to/file.go",
				"line":  110,
				"msg":   "test message",
			},
			want: false,
		},
		{
			name: "Error: missing level key",
			m: map[string]any{
				"time": "2025-09-09T18:23:06.102045-07:00",
				"file": "/path/to/file.go",
				"line": 110,
				"msg":  "test message",
			},
			want: false,
		},
		{
			name: "Error: missing file key",
			m: map[string]any{
				"time":  "2025-09-09T18:23:06.102045-07:00",
				"level": "ERROR",
				"line":  110,
				"msg":   "test message",
			},
			want: false,
		},
		{
			name: "Error: missing line key",
			m: map[string]any{
				"time":  "2025-09-09T18:23:06.102045-07:00",
				"level": "ERROR",
				"file":  "/path/to/file.go",
				"msg":   "test message",
			},
			want: false,
		},
		{
			name: "Error: missing msg key",
			m: map[string]any{
				"time":  "2025-09-09T18:23:06.102045-07:00",
				"level": "ERROR",
				"file":  "/path/to/file.go",
				"line":  110,
			},
			want: false,
		},
		{
			name: "Error: empty map",
			m:    map[string]any{},
			want: false,
		},
	}

	for _, test := range tests {
		holder := mapHolder{m: test.m}
		got := holder.valid()
		if got != test.want {
			t.Errorf("TestMapHolderValid(%s): got %v, want %v", test.name, got, test.want)
		}

	}
}

func TestMapHolderLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    map[string]any
		want string
	}{
		{
			name: "Success: valid string level",
			m:    map[string]any{"level": "error"},
			want: "ERROR",
		},
		{
			name: "Success: another valid string level",
			m:    map[string]any{"level": "info"},
			want: "INFO",
		},
		{
			name: "Success: debug level",
			m:    map[string]any{"level": "debug"},
			want: "DEBUG",
		},
		{
			name: "Error: non-string level",
			m:    map[string]any{"level": 123},
			want: "UnknownLevel",
		},
		{
			name: "Error: missing level key",
			m:    map[string]any{},
			want: "UnknownLevel",
		},
		{
			name: "Error: nil level value",
			m:    map[string]any{"level": nil},
			want: "UnknownLevel",
		},
	}

	for _, test := range tests {
		holder := mapHolder{m: test.m}
		got := holder.level()
		if got != test.want {
			t.Errorf("TestMapHolderLevel(%s): got %s, want %s", test.name, got, test.want)
		}
	}
}

func TestMapHolderLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    map[string]any
		want int
	}{
		{
			name: "Success: valid line number",
			m:    map[string]any{"line": 110},
			want: 110,
		},
		{
			name: "Success: zero line number",
			m:    map[string]any{"line": 0},
			want: 0,
		},
		{
			name: "Success: large line number",
			m:    map[string]any{"line": 99999},
			want: 99999,
		},
		{
			name: "Error: non-int line",
			m:    map[string]any{"line": "110"},
			want: 0,
		},
		{
			name: "Error: missing line key",
			m:    map[string]any{},
			want: 0,
		},
		{
			name: "Error: nil line value",
			m:    map[string]any{"line": nil},
			want: 0,
		},
		{
			name: "Success: float line value gets converted to int",
			m:    map[string]any{"line": 110.0},
			want: 110,
		},
		{
			name: "Success: float line value with fractional part gets truncated",
			m:    map[string]any{"line": 110.7},
			want: 110,
		},
	}

	for _, test := range tests {
		holder := mapHolder{m: test.m}
		got := holder.line()
		if got != test.want {
			t.Errorf("TestMapHolderLine(%s): got %d, want %d", test.name, got, test.want)
		}
	}
}

func TestMapHolderShortFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    map[string]any
		want string
	}{
		{
			name: "Success: full path with multiple directories",
			m:    map[string]any{"file": "/path/to/very/long/directory/structure/file.go"},
			want: "file.go",
		},
		{
			name: "Success: simple path",
			m:    map[string]any{"file": "/path/file.go"},
			want: "file.go",
		},
		{
			name: "Success: no directory separators",
			m:    map[string]any{"file": "file.go"},
			want: "file.go",
		},
		{
			name: "Success: relative path",
			m:    map[string]any{"file": "./dir/file.go"},
			want: "file.go",
		},
		{
			name: "Error: non-string file",
			m:    map[string]any{"file": 123},
			want: "unknown",
		},
		{
			name: "Error: missing file key",
			m:    map[string]any{},
			want: "unknown",
		},
		{
			name: "Error: nil file value",
			m:    map[string]any{"file": nil},
			want: "unknown",
		},
		{
			name: "Success: empty string file",
			m:    map[string]any{"file": ""},
			want: "",
		},
	}

	for _, test := range tests {
		holder := mapHolder{m: test.m}
		got := holder.shortFile()
		if got != test.want {
			t.Errorf("TestMapHolderShortFile(%s): got %s, want %s", test.name, got, test.want)
		}
	}
}

func TestMapHolderHourMinuteSecond(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    map[string]any
		want string
	}{
		{
			name: "Success: valid RFC3339 timestamp",
			m:    map[string]any{"time": "2025-09-09T18:23:06.102045-07:00"},
			want: "18:23:06",
		},
		{
			name: "Success: UTC timestamp",
			m:    map[string]any{"time": "2025-09-09T18:23:06Z"},
			want: "18:23:06",
		},
		{
			name: "Success: different timezone",
			m:    map[string]any{"time": "2025-09-09T15:30:45+03:00"},
			want: "15:30:45",
		},
		{
			name: "Success: midnight",
			m:    map[string]any{"time": "2025-09-09T00:00:00Z"},
			want: "00:00:00",
		},
		{
			name: "Error: invalid time format",
			m:    map[string]any{"time": "2025-09-09 18:23:06"},
			want: "00:00:00",
		},
		{
			name: "Error: non-string time",
			m:    map[string]any{"time": 1234567890},
			want: "00:00:00",
		},
		{
			name: "Error: missing time key",
			m:    map[string]any{},
			want: "00:00:00",
		},
		{
			name: "Error: nil time value",
			m:    map[string]any{"time": nil},
			want: "00:00:00",
		},
		{
			name: "Error: empty string time",
			m:    map[string]any{"time": ""},
			want: "00:00:00",
		},
		{
			name: "Error: completely invalid time string",
			m:    map[string]any{"time": "not a time"},
			want: "00:00:00",
		},
	}

	for _, test := range tests {
		holder := mapHolder{m: test.m}
		got := holder.hourMinuteSecond()
		if got != test.want {
			t.Errorf("TestMapHolderHourMinuteSecond(%s): got %s, want %s", test.name, got, test.want)
		}
	}
}

func TestMapHolderMsg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    map[string]any
		want string
	}{
		{
			name: "Success: valid string message",
			m:    map[string]any{"msg": "test message"},
			want: "test message",
		},
		{
			name: "Success: empty string message",
			m:    map[string]any{"msg": ""},
			want: "",
		},
		{
			name: "Error: non-string message",
			m:    map[string]any{"msg": 123},
			want: "no message",
		},
		{
			name: "Error: missing msg key",
			m:    map[string]any{},
			want: "no message",
		},
		{
			name: "Error: nil msg value",
			m:    map[string]any{"msg": nil},
			want: "no message",
		},
	}

	for _, test := range tests {
		holder := mapHolder{m: test.m}
		got := holder.msg()
		if got != test.want {
			t.Errorf("TestMapHolderMsg(%s): got %s, want %s", test.name, got, test.want)
		}
	}
}

func TestReframe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line []byte
		want []byte
	}{
		{
			name: "Success: valid JSON log line",
			line: []byte(`{"time":"2025-09-09T18:23:06.102045-07:00","level":"ERROR","file":"/path/to/checker.go","line":110,"msg":"failed to read configuration file"}`),
			want: []byte("[ERROR][18:23:06][.../checker.go:110]: failed to read configuration file"),
		},
		{
			name: "Success: different level and time",
			line: []byte(`{"time":"2025-09-09T15:30:45Z","level":"info","file":"/some/other/file.go","line":25,"msg":"processing request"}`),
			want: []byte("[INFO][15:30:45][.../file.go:25]: processing request"),
		},
		{
			name: "Success: debug level",
			line: []byte(`{"time":"2025-09-09T09:15:30.123Z","level":"debug","file":"/debug/test.go","line":1,"msg":"debug message"}`),
			want: []byte("[DEBUG][09:15:30][.../test.go:1]: debug message"),
		},
		{
			name: "Error: invalid JSON returns original",
			line: []byte(`not valid json`),
			want: []byte(`not valid json`),
		},
		{
			name: "Error: missing required fields returns original",
			line: []byte(`{"time":"2025-09-09T18:23:06.102045-07:00","level":"ERROR"}`),
			want: []byte(`{"time":"2025-09-09T18:23:06.102045-07:00","level":"ERROR"}`),
		},
		{
			name: "Error: empty JSON object returns original",
			line: []byte(`{}`),
			want: []byte(`{}`),
		},
		{
			name: "Error: valid JSON but wrong structure returns original",
			line: []byte(`{"foo": "bar", "baz": 123}`),
			want: []byte(`{"foo": "bar", "baz": 123}`),
		},
		{
			name: "Error: empty line returns original",
			line: []byte(``),
			want: []byte(``),
		},
		{
			name: "Success: extra fields are ignored",
			line: []byte(`{"time":"2025-09-09T12:00:00Z","level":"warn","file":"/test.go","line":42,"msg":"warning","extra":"ignored","source":{"function":"test"}}`),
			want: []byte("[WARN][12:00:00][.../test.go:42]: warning"),
		},
	}

	for _, test := range tests {
		got := reframe(t.Context(), test.line)
		if diff := pretty.Compare(got, test.want); diff != "" {
			t.Errorf("TestReframe(%s): -got +want:\n%s", test.name, diff)
		}
	}
}

func TestMapHolderReset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    map[string]any
	}{
		{
			name: "Success: reset map with data",
			m: map[string]any{
				"time":  "2025-09-09T18:23:06.102045-07:00",
				"level": "ERROR",
				"file":  "/path/to/file.go",
				"line":  110,
				"msg":   "test message",
			},
		},
		{
			name: "Success: reset empty map",
			m:    map[string]any{},
		},
	}

	for _, test := range tests {
		holder := mapHolder{m: test.m}
		holder.Reset()
		if len(holder.m) != 0 {
			t.Errorf("TestMapHolderReset(%s): expected empty map after reset, got %d items", test.name, len(holder.m))
		}
	}
}

func TestMapHolderMarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		m       map[string]any
		wantErr bool
	}{
		{
			name: "Success: marshal simple map",
			m: map[string]any{
				"level": "ERROR",
				"msg":   "test message",
			},
			wantErr: false,
		},
		{
			name:    "Success: marshal empty map",
			m:       map[string]any{},
			wantErr: false,
		},
		{
			name: "Success: marshal complex map",
			m: map[string]any{
				"time":  "2025-09-09T18:23:06.102045-07:00",
				"level": "ERROR",
				"file":  "/path/to/file.go",
				"line":  110,
				"msg":   "test message",
				"extra": map[string]any{"nested": "value"},
			},
			wantErr: false,
		},
	}

	for _, test := range tests {

		holder := mapHolder{m: test.m}
		_, err := holder.MarshalJSON()
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestMapHolderMarshalJSON(%s): got err == nil, want err != nil", test.name)
			return
		case err != nil && !test.wantErr:
			t.Errorf("TestMapHolderMarshalJSON(%s): got err == %s, want err == nil", test.name, err)
			return
		case err != nil:
			return
		}
	}
}

func TestScan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "Success: single valid JSON log line",
			input: `{"time":"2025-09-09T18:23:06.102045-07:00","level":"ERROR","file":"/path/to/checker.go","line":110,"msg":"failed to read configuration file"}
`,
			want: "[ERROR][18:23:06][.../checker.go:110]: failed to read configuration file\n",
		},
		{
			name: "Success: multiple valid JSON log lines",
			input: `{"time":"2025-09-09T18:23:06.102045-07:00","level":"ERROR","file":"/path/to/checker.go","line":110,"msg":"failed to read configuration file"}
{"time":"2025-09-09T15:30:45Z","level":"info","file":"/some/other/file.go","line":25,"msg":"processing request"}
`,
			want: "[ERROR][18:23:06][.../checker.go:110]: failed to read configuration file\n[INFO][15:30:45][.../file.go:25]: processing request\n",
		},
		{
			name: "Success: mix of valid and invalid lines",
			input: `{"time":"2025-09-09T18:23:06.102045-07:00","level":"ERROR","file":"/path/to/checker.go","line":110,"msg":"failed to read configuration file"}
not valid json line
{"time":"2025-09-09T15:30:45Z","level":"info","file":"/some/other/file.go","line":25,"msg":"processing request"}
`,
			want: "[ERROR][18:23:06][.../checker.go:110]: failed to read configuration file\nnot valid json line\n[INFO][15:30:45][.../file.go:25]: processing request\n",
		},
		{
			name: "Success: only invalid lines pass through unchanged",
			input: `not valid json line
another invalid line
yet another line
`,
			want: "not valid json line\nanother invalid line\nyet another line\n",
		},
		{
			name:  "Success: empty input",
			input: "",
			want:  "",
		},
		{
			name: "Success: lines missing required fields pass through unchanged",
			input: `{"time":"2025-09-09T18:23:06.102045-07:00","level":"ERROR"}
{"foo": "bar", "baz": 123}
`,
			want: `{"time":"2025-09-09T18:23:06.102045-07:00","level":"ERROR"}
{"foo": "bar", "baz": 123}
`,
		},
		{
			name:  "Success: single line without newline",
			input: `{"time":"2025-09-09T18:23:06.102045-07:00","level":"ERROR","file":"/path/to/checker.go","line":110,"msg":"test"}`,
			want:  "[ERROR][18:23:06][.../checker.go:110]: test\n",
		},
		{
			name: "Success: different log levels",
			input: `{"time":"2025-09-09T12:00:00Z","level":"debug","file":"/test.go","line":1,"msg":"debug message"}
{"time":"2025-09-09T12:01:00Z","level":"warn","file":"/test.go","line":2,"msg":"warning message"}
{"time":"2025-09-09T12:02:00Z","level":"error","file":"/test.go","line":3,"msg":"error message"}
`,
			want: "[DEBUG][12:00:00][.../test.go:1]: debug message\n[WARN][12:01:00][.../test.go:2]: warning message\n[ERROR][12:02:00][.../test.go:3]: error message\n",
		},
	}

	for _, test := range tests {
		input := strings.NewReader(test.input)
		var output bytes.Buffer

		scan(t.Context(), input, &output)

		got := output.String()
		if got != test.want {
			t.Errorf("TestScan(%s): got %q, want %q", test.name, got, test.want)
		}
	}
}
