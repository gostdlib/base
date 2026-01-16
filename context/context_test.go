package context

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"testing"

	"github.com/gostdlib/base/concurrency/background"
	"github.com/gostdlib/base/telemetry/log"
	"github.com/gostdlib/base/telemetry/otel/metrics"

	"go.opentelemetry.io/otel/metric"
)

func TestBackground(t *testing.T) {
	ctx := Background()

	if ctx.Value(loggerKey{}) != log.Default() {
		t.Errorf("TestBackground: Logger not attached to context")
	}

	if ctx.Value(metricsKey{}) != metrics.Default() {
		t.Errorf("TestBackground: Metrics not attached to context")
	}
}

func TestLog(t *testing.T) {
	defaultLogger := log.Default()

	tests := []struct {
		name string
		ctx  context.Context
		want Logger
	}{
		{
			name: "LoggerAttached",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, loggerKey{}, defaultLogger)
			}(),
			want: Logger{logger: defaultLogger},
		},
		{
			name: "NoLoggerAttached",
			ctx:  context.Background(),
			want: Logger{logger: defaultLogger},
		},
		{
			name: "InvalidLoggerType",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, loggerKey{}, "invalid")
			}(),
			want: Logger{logger: defaultLogger},
		},
	}

	for _, test := range tests {
		got := Log(test.ctx)
		test.want.ctx = test.ctx
		if got != test.want {
			t.Errorf("TestLog() = %v, want %v", got, test.want)
		}
	}
}

func TestMetrics(t *testing.T) {
	defaultProvider := metrics.Default()

	tests := []struct {
		name string
		ctx  context.Context
		want metric.MeterProvider
	}{
		{
			name: "MetricsProviderAttached",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, metricsKey{}, defaultProvider)
			}(),
			want: defaultProvider,
		},
		{
			name: "NoAuditClientAttached",
			ctx:  context.Background(),
			want: defaultProvider,
		},
		{
			name: "InvalidAuditClientType",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, metricsKey{}, "invalid")
			}(),
			want: defaultProvider,
		},
	}

	for _, test := range tests {
		got := MeterProvider(test.ctx)
		if got != test.want {
			t.Errorf("TestMetrics(%s): MeterProvider() = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestTasks(t *testing.T) {
	defaultTasks := background.Default()

	tests := []struct {
		name string
		ctx  context.Context
		want *background.Tasks
	}{
		{
			name: "ClientAttached",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, tasksKey{}, defaultTasks)
			}(),
			want: defaultTasks,
		},
		{
			name: "NoAuditClientAttached",
			ctx:  context.Background(),
			want: defaultTasks,
		},
		{
			name: "InvalidAuditClientType",
			ctx: func() context.Context {
				ctx := context.Background()
				return context.WithValue(ctx, tasksKey{}, "invalid")
			}(),
			want: defaultTasks,
		},
	}

	for _, test := range tests {
		got := Tasks(test.ctx)
		if got != test.want {
			t.Errorf("TestTasks() = %v, want %v", got, test.want)
		}
	}
}

func TestShouldTrace(t *testing.T) {
	ctx := context.Background()

	if ShouldTrace(ctx) {
		t.Fatalf("TestShouldTrace: ShouldTrace() = true, want false")
	}

	ctx = SetShouldTrace(ctx, true)
	if !ShouldTrace(ctx) {
		t.Fatalf("TestShouldTrace: ShouldTrace() = false, want true")
	}
}

func TestAttrs(t *testing.T) {
	tests := []struct {
		name      string
		setup     func() context.Context
		wantAttrs []slog.Attr
	}{
		{
			name: "Success: Add multiple attributes to empty context",
			setup: func() context.Context {
				ctx := context.Background()
				return AddAttrs(ctx, slog.String("key1", "value1"), slog.Int("key2", 42))
			},
			wantAttrs: []slog.Attr{slog.String("key1", "value1"), slog.Int("key2", 42)},
		},
		{
			name: "Success: Add attributes to context with existing attributes",
			setup: func() context.Context {
				ctx := context.Background()
				ctx = AddAttrs(ctx, slog.String("key1", "value1"))
				return AddAttrs(ctx, slog.String("key2", "value2"))
			},
			wantAttrs: []slog.Attr{slog.String("key1", "value1"), slog.String("key2", "value2")},
		},
		{
			name: "Success: Get attributes from empty context",
			setup: func() context.Context {
				return context.Background()
			},
			wantAttrs: nil,
		},
	}

	for _, test := range tests {
		ctx := test.setup()
		got := Attrs(ctx)

		if len(got) != len(test.wantAttrs) {
			t.Errorf("TestAttrs(%s): got %d attrs, want %d attrs", test.name, len(got), len(test.wantAttrs))
			continue
		}

		for i := range got {
			if !got[i].Equal(test.wantAttrs[i]) {
				t.Errorf("TestAttrs(%s): attr[%d] = %v, want %v", test.name, i, got[i], test.wantAttrs[i])
			}
		}
	}
}

// fakeHandler is a slog.Handler that captures log records for testing.
type fakeHandler struct {
	records []slog.Record
	enabled bool
}

func (h *fakeHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return h.enabled
}

func (h *fakeHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *fakeHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *fakeHandler) WithGroup(_ string) slog.Handler {
	return h
}

// recordAttrs extracts all attributes from a slog.Record into a slice.
func recordAttrs(r slog.Record) []slog.Attr {
	var attrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	return attrs
}

func TestInternalLog(t *testing.T) {
	tests := []struct {
		name          string
		ctxAttrs      []slog.Attr
		logArgs       []any
		wantAttrCount int
		wantAttrs     map[string]any
	}{
		{
			name:          "Success: context attrs are included in log output",
			ctxAttrs:      []slog.Attr{slog.String("request_id", "abc123"), slog.Int("user_id", 42)},
			logArgs:       []any{},
			wantAttrCount: 2,
			wantAttrs:     map[string]any{"request_id": "abc123", "user_id": int64(42)},
		},
		{
			name:          "Success: context attrs combined with log args",
			ctxAttrs:      []slog.Attr{slog.String("trace_id", "xyz789")},
			logArgs:       []any{"operation", "test"},
			wantAttrCount: 2,
			wantAttrs:     map[string]any{"trace_id": "xyz789", "operation": "test"},
		},
		{
			name:          "Success: no context attrs with log args only",
			ctxAttrs:      nil,
			logArgs:       []any{"key", "value"},
			wantAttrCount: 1,
			wantAttrs:     map[string]any{"key": "value"},
		},
		{
			name:          "Success: empty context and no log args",
			ctxAttrs:      nil,
			logArgs:       []any{},
			wantAttrCount: 0,
			wantAttrs:     map[string]any{},
		},
	}

	for _, test := range tests {
		handler := &fakeHandler{enabled: true}

		ctx := context.Background()
		if len(test.ctxAttrs) > 0 {
			ctx = AddAttrs(ctx, test.ctxAttrs...)
		}

		logger := Logger{ctx: ctx, logger: slog.New(handler)}

		logger.log(slog.LevelInfo, "test message", test.logArgs...)

		if len(handler.records) != 1 {
			t.Errorf("TestLog(%s): got %d records, want 1", test.name, len(handler.records))
			continue
		}

		attrs := recordAttrs(handler.records[0])
		if len(attrs) != test.wantAttrCount {
			t.Errorf("TestLog(%s): got %d attrs, want %d", test.name, len(attrs), test.wantAttrCount)
			continue
		}

		for _, attr := range attrs {
			want, ok := test.wantAttrs[attr.Key]
			if !ok {
				t.Errorf("TestLog(%s): unexpected attr key %q", test.name, attr.Key)
				continue
			}
			if attr.Value.Any() != want {
				t.Errorf("TestLog(%s): attr %q = %v, want %v", test.name, attr.Key, attr.Value.Any(), want)
			}
		}
	}

	// Verify caller info is correct when using public API.
	handler := &fakeHandler{enabled: true}
	logger := Logger{logger: slog.New(handler)}
	_, _, wantLine, _ := runtime.Caller(0)
	logger.Info("caller test") // wantLine + 1
	wantLine++

	record := handler.records[0]
	fs := runtime.CallersFrames([]uintptr{record.PC})
	frame, _ := fs.Next()
	if !strings.HasSuffix(frame.File, "context_test.go") {
		t.Errorf("TestLoggerLogContextAttrs(caller): got file %q, want suffix \"context_test.go\"", frame.File)
	}
	if frame.Line != wantLine {
		t.Errorf("TestLoggerLogContextAttrs(caller): got line %d, want %d", frame.Line, wantLine)
	}
}

func TestLogAttrs(t *testing.T) {
	tests := []struct {
		name             string
		ctxAttrs         []slog.Attr
		logAttrs         []slog.Attr
		wantAttrCount    int
		wantAttrs        map[string]any // Used when order doesn't matter
		wantAttrsOrdered []slog.Attr    // Used to verify exact order of attrs
	}{
		{
			name:          "Success: context attrs are included in logAttrs output",
			ctxAttrs:      []slog.Attr{slog.String("request_id", "abc123"), slog.Int("user_id", 42)},
			logAttrs:      []slog.Attr{},
			wantAttrCount: 2,
			wantAttrs:     map[string]any{"request_id": "abc123", "user_id": int64(42)},
		},
		{
			name:          "Success: context attrs combined with passed attrs",
			ctxAttrs:      []slog.Attr{slog.String("trace_id", "xyz789")},
			logAttrs:      []slog.Attr{slog.String("operation", "test")},
			wantAttrCount: 2,
			wantAttrs:     map[string]any{"trace_id": "xyz789", "operation": "test"},
		},
		{
			name:          "Success: no context attrs with passed attrs only",
			ctxAttrs:      nil,
			logAttrs:      []slog.Attr{slog.String("key", "value")},
			wantAttrCount: 1,
			wantAttrs:     map[string]any{"key": "value"},
		},
		{
			name:          "Success: empty context and no passed attrs",
			ctxAttrs:      nil,
			logAttrs:      []slog.Attr{},
			wantAttrCount: 0,
			wantAttrs:     map[string]any{},
		},
		{
			name:          "Success: same key in context and passed attrs",
			ctxAttrs:      []slog.Attr{slog.String("key", "ctx_value")},
			logAttrs:      []slog.Attr{slog.String("key", "passed_value")},
			wantAttrCount: 2,
			wantAttrsOrdered: []slog.Attr{
				slog.String("key", "ctx_value"),    // Context attr comes first
				slog.String("key", "passed_value"), // Passed attr comes second (wins at render time)
			},
		},
		{
			name:          "Success: multiple overlapping keys with different values",
			ctxAttrs:      []slog.Attr{slog.String("id", "ctx_id"), slog.Int("count", 1)},
			logAttrs:      []slog.Attr{slog.String("id", "passed_id"), slog.Int("count", 99)},
			wantAttrCount: 4,
			wantAttrsOrdered: []slog.Attr{
				slog.String("id", "ctx_id"), // Context attrs first
				slog.Int("count", 1),
				slog.String("id", "passed_id"), // Passed attrs second (win at render time)
				slog.Int("count", 99),
			},
		},
	}

	for _, test := range tests {
		handler := &fakeHandler{enabled: true}
		logger := Logger{logger: slog.New(handler)}

		ctx := context.Background()
		if len(test.ctxAttrs) > 0 {
			ctx = AddAttrs(ctx, test.ctxAttrs...)
		}

		logger.logAttrs(ctx, slog.LevelInfo, "test message", test.logAttrs...)

		if len(handler.records) != 1 {
			t.Errorf("TestLogAttrs(%s): got %d records, want 1", test.name, len(handler.records))
			continue
		}

		attrs := recordAttrs(handler.records[0])
		if len(attrs) != test.wantAttrCount {
			t.Errorf("TestLogAttrs(%s): got %d attrs, want %d", test.name, len(attrs), test.wantAttrCount)
			continue
		}

		// Use ordered verification when wantAttrsOrdered is set (for duplicate key tests)
		if test.wantAttrsOrdered != nil {
			for i, want := range test.wantAttrsOrdered {
				if !attrs[i].Equal(want) {
					t.Errorf("TestLogAttrs(%s): attr[%d] = %v, want %v", test.name, i, attrs[i], want)
				}
			}
			continue
		}

		// Build a map of last-seen values (naturally deduplicates with last-wins)
		gotAttrs := make(map[string]any)
		for _, attr := range attrs {
			gotAttrs[attr.Key] = attr.Value.Any()
		}
		for key, want := range test.wantAttrs {
			got, ok := gotAttrs[key]
			if !ok {
				t.Errorf("TestLogAttrs(%s): missing attr key %q", test.name, key)
				continue
			}
			if got != want {
				t.Errorf("TestLogAttrs(%s): attr %q = %v, want %v", test.name, key, got, want)
			}
		}
		for key := range gotAttrs {
			if _, ok := test.wantAttrs[key]; !ok {
				t.Errorf("TestLogAttrs(%s): unexpected attr key %q", test.name, key)
			}
		}
	}

	// Verify caller info is correct when using public API.
	handler := &fakeHandler{enabled: true}
	logger := Logger{logger: slog.New(handler)}
	_, _, wantLine, _ := runtime.Caller(0)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "caller test") // wantLine + 1
	wantLine++

	record := handler.records[0]
	fs := runtime.CallersFrames([]uintptr{record.PC})
	frame, _ := fs.Next()
	if !strings.HasSuffix(frame.File, "context_test.go") {
		t.Errorf("TestLoggerLogAttrsContextAttrs(caller): got file %q, want suffix \"context_test.go\"", frame.File)
	}
	if frame.Line != wantLine {
		t.Errorf("TestLoggerLogAttrsContextAttrs(caller): got line %d, want %d", frame.Line, wantLine)
	}
}
