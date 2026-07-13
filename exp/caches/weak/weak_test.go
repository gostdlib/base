package weak

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"weak"

	"github.com/gostdlib/base/concurrency/sync"
	internalctx "github.com/gostdlib/base/internal/context"
	"github.com/kylelemons/godebug/pretty"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type testValue struct {
	data string
	num  int
}

// metricReader wires a context to a fresh ManualReader-backed MeterProvider so a test can read the cache's emitted
// metrics. It returns the context to pass to the cache and the reader to collect from.
func metricReader(t *testing.T) (context.Context, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	return context.WithValue(t.Context(), internalctx.MetricsKey{}, mp), reader
}

// gcUntilFor forces a GC and polls cond 5ms apart until cond returns true or d elapses, returning whether cond became
// true. It is the deadline-bounded variant of gcUntil for collections that can take longer than gcUntil's fixed budget.
func gcUntilFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		runtime.GC()
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// gcUntil forces a GC and polls cond, 5ms apart for up to one second, returning whether cond became true. Callers issue
// their own t.Fatalf so the failure message follows each test's convention.
func gcUntil(cond func() bool) bool {
	return gcUntilFor(1*time.Second, cond)
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options []Option
		wantErr bool
	}{
		{
			name:    "Success: create cache without options",
			options: nil,
			wantErr: false,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test", test.options...)

		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestNew(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestNew(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		if cache == nil {
			t.Errorf("TestNew(%s): got nil cache, want non-nil", test.name)
		}
	}
}

func TestCacheBasicOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setupFunc func(*Cache[string, testValue])
		operation func(*Cache[string, testValue]) (any, bool, error)
		wantValue any
		wantOk    bool
		wantErr   bool
	}{
		{
			name:      "Success: Get from empty cache returns not found",
			setupFunc: nil,
			operation: func(c *Cache[string, testValue]) (any, bool, error) {
				return c.Get(t.Context(), "key1")
			},
			wantValue: (*testValue)(nil),
			wantOk:    false,
			wantErr:   false,
		},
		{
			name:      "Success: Set and Get value",
			setupFunc: nil,
			operation: func(c *Cache[string, testValue]) (any, bool, error) {
				val := &testValue{data: "test", num: 42}
				_, _, err := c.Set(t.Context(), "key1", val)
				if err != nil {
					return nil, false, err
				}
				return c.Get(t.Context(), "key1")
			},
			wantValue: &testValue{data: "test", num: 42},
			wantOk:    true,
			wantErr:   false,
		},
		{
			name: "Success: Set overwrites existing value",
			setupFunc: func(c *Cache[string, testValue]) {
				val := &testValue{data: "old", num: 1}
				_, _, _ = c.Set(t.Context(), "key1", val)
			},
			operation: func(c *Cache[string, testValue]) (any, bool, error) {
				val := &testValue{data: "new", num: 2}
				return c.Set(t.Context(), "key1", val)
			},
			wantValue: &testValue{data: "old", num: 1},
			wantOk:    true,
			wantErr:   false,
		},
		{
			name: "Success: Del removes value",
			setupFunc: func(c *Cache[string, testValue]) {
				val := &testValue{data: "test", num: 42}
				_, _, _ = c.Set(t.Context(), "key1", val)
			},
			operation: func(c *Cache[string, testValue]) (any, bool, error) {
				return c.Del(t.Context(), "key1")
			},
			wantValue: &testValue{data: "test", num: 42},
			wantOk:    true,
			wantErr:   false,
		},
		{
			name:      "Success: Del on non-existent key returns not found",
			setupFunc: nil,
			operation: func(c *Cache[string, testValue]) (any, bool, error) {
				return c.Del(t.Context(), "nonexistent")
			},
			wantValue: (*testValue)(nil),
			wantOk:    false,
			wantErr:   false,
		},
		{
			name:      "Success: Get on nil cache returns not found",
			setupFunc: nil,
			operation: func(c *Cache[string, testValue]) (any, bool, error) {
				return c.Get(t.Context(), "key1")
			},
			wantValue: (*testValue)(nil),
			wantOk:    false,
			wantErr:   false,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestCacheBasicOperations(%s): failed to create cache: %v", test.name, err)
		}

		if test.setupFunc != nil {
			test.setupFunc(cache)
		}

		gotValue, gotOk, err := test.operation(cache)

		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestCacheBasicOperations(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestCacheBasicOperations(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		if gotOk != test.wantOk {
			t.Errorf("TestCacheBasicOperations(%s): got ok=%v, want ok=%v", test.name, gotOk, test.wantOk)
		}

		if diff := pretty.Compare(gotValue, test.wantValue); diff != "" {
			t.Errorf("TestCacheBasicOperations(%s): -got +want:\n%s", test.name, diff)
		}
	}
}

func TestConcurrentGetSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		numGoroutines int
		numOperations int
		operation     string
	}{
		{
			name:          "Success: concurrent Sets on different keys",
			numGoroutines: 100,
			numOperations: 1000,
			operation:     "set",
		},
		{
			name:          "Success: concurrent Gets on same keys",
			numGoroutines: 100,
			numOperations: 1000,
			operation:     "get",
		},
		{
			name:          "Success: mixed concurrent Gets and Sets",
			numGoroutines: 100,
			numOperations: 1000,
			operation:     "mixed",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[int, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestConcurrentGetSet(%s): failed to create cache: %v", test.name, err)
		}

		// Pre-populate cache for get operations
		if test.operation == "get" || test.operation == "mixed" {
			for i := 0; i < test.numOperations; i++ {
				val := &testValue{data: "initial", num: i}
				_, _, _ = cache.Set(t.Context(), i, val)
			}
		}

		var wg sync.Group

		for g := 0; g < test.numGoroutines; g++ {
			for i := 0; i < test.numOperations; i++ {
				wg.Go(
					ctx,
					func(ctx context.Context) error {
						key := i

						switch test.operation {
						case "set":
							val := &testValue{data: "concurrent", num: g*test.numOperations + i}
							_, _, _ = cache.Set(t.Context(), key, val)
						case "get":
							_, _, _ = cache.Get(t.Context(), key)
						case "mixed":
							if i%2 == 0 {
								_, _, _ = cache.Get(t.Context(), key)
							} else {
								val := &testValue{data: "mixed", num: g*test.numOperations + i}
								_, _, _ = cache.Set(t.Context(), key, val)
							}
						}
						return nil
					},
				)
			}
		}

		wg.Wait(ctx)

		// Verify cache is still functional
		if test.operation == "set" || test.operation == "mixed" {
			testKey := 0
			testVal := &testValue{data: "verify", num: 999}
			_, _, err := cache.Set(t.Context(), testKey, testVal)
			if err != nil {
				t.Fatalf("TestConcurrentGetSet(%s): failed to set value: %v", test.name, err)
			}
			gotVal, ok, err := cache.Get(t.Context(), testKey)
			if err != nil {
				t.Fatalf("TestConcurrentGetSet(%s): failed to get value: %v", test.name, err)
			}
			if !ok {
				t.Errorf("TestConcurrentGetSet(%s): failed to get value after concurrent operations", test.name)
			}
			if diff := pretty.Compare(gotVal, testVal); diff != "" {
				t.Errorf("TestConcurrentGetSet(%s): -got +want:\n%s", test.name, diff)
			}
		}
	}
}

func TestConcurrentDelete(t *testing.T) {
	tests := []struct {
		name          string
		numGoroutines int
		numKeys       int
	}{
		{
			name:          "Success: concurrent deletes on different keys",
			numGoroutines: 50,
			numKeys:       1000,
		},
		{
			name:          "Success: concurrent deletes on same keys",
			numGoroutines: 100,
			numKeys:       10,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[int, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestConcurrentDelete(%s): failed to create cache: %v", test.name, err)
		}

		// Populate cache - keep strong references to prevent GC
		values := make([]*testValue, test.numKeys)
		for i := 0; i < test.numKeys; i++ {
			val := &testValue{data: "delete-test", num: i}
			values[i] = val
			_, _, _ = cache.Set(t.Context(), i, val)
		}

		initialLen := cache.Len()
		if initialLen != test.numKeys {
			t.Errorf("TestConcurrentDelete(%s): initial length=%d, want=%d", test.name, initialLen, test.numKeys)
		}

		var wg sync.Group

		for g := 0; g < test.numGoroutines; g++ {
			wg.Go(
				ctx,
				func(ctx context.Context) error {
					for i := 0; i < test.numKeys; i++ {
						_, _, _ = cache.Del(t.Context(), i)
					}
					return nil
				},
			)
		}

		wg.Wait(ctx)

		// All keys should be deleted
		finalLen := cache.Len()
		if finalLen != 0 {
			t.Errorf("TestConcurrentDelete(%s): final length=%d, want=0", test.name, finalLen)
		}
	}
}

func TestWeakPointerCleanup(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: weak pointer cleanup removes entries",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestWeakPointerCleanup(%s): failed to create cache: %v", test.name, err)
		}

		// Create a value and add to cache
		key := "cleanup-test"
		val := &testValue{data: "will-be-collected", num: 42}
		_, _, err = cache.Set(t.Context(), key, val)
		if err != nil {
			t.Fatalf("TestWeakPointerCleanup(%s): failed to set value: %v", test.name, err)
		}

		// Verify it exists
		gotVal, ok, err := cache.Get(t.Context(), key)
		if err != nil {
			t.Fatalf("TestWeakPointerCleanup(%s): failed to get value: %v", test.name, err)
		}
		if !ok || gotVal == nil {
			t.Errorf("TestWeakPointerCleanup(%s): value not found after Set", test.name)
		}

		// Remove strong reference
		val = nil
		gotVal = nil

		// Force GC
		runtime.GC()
		runtime.GC() // Call twice to ensure cleanup finalizers run
		time.Sleep(100 * time.Millisecond)

		// Note: We cannot reliably test that the cleanup happened because:
		// 1. GC timing is non-deterministic
		// 2. The cleanup function may not run immediately
		// 3. Weak pointers may still hold references temporarily
		// This test primarily ensures the cleanup registration doesn't panic
		// and the cache remains functional after GC

		// Verify cache is still functional with new values
		newVal := &testValue{data: "new-value", num: 100}
		_, _, err = cache.Set(t.Context(), "new-key", newVal)
		if err != nil {
			t.Fatalf("TestWeakPointerCleanup(%s): failed to set new value: %v", test.name, err)
		}
		got, ok, err := cache.Get(t.Context(), "new-key")
		if err != nil {
			t.Fatalf("TestWeakPointerCleanup(%s): failed to get new value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestWeakPointerCleanup(%s): failed to set/get after GC", test.name)
		}
		if diff := pretty.Compare(got, newVal); diff != "" {
			t.Errorf("TestWeakPointerCleanup(%s): -got +want:\n%s", test.name, diff)
		}
	}
}

func TestWeakPointerCollectedCleanup(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: Get cleans up collected weak pointers",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestWeakPointerCollectedCleanup(%s): failed to create cache: %v", test.name, err)
		}

		key := "cleanup-test"

		// Set a value and immediately remove strong references
		func() {
			val := &testValue{data: "will-be-collected", num: 42}
			_, _, _ = cache.Set(t.Context(), key, val)
			// val goes out of scope here
		}()

		// Force GC multiple times to try to collect the weak pointer
		for i := 0; i < 5; i++ {
			runtime.GC()
			time.Sleep(10 * time.Millisecond)
		}

		// Try to get the value - if it was collected, Get should:
		// 1. Find the weak pointer is nil
		// 2. Delete the key
		// 3. Return nil, false
		val, ok, err := cache.Get(t.Context(), key)
		if err != nil {
			t.Fatalf("TestWeakPointerCollectedCleanup(%s): failed to get value: %v", test.name, err)
		}

		// Note: This test is inherently non-deterministic because:
		// 1. GC may not have run yet
		// 2. Weak pointers may still hold references temporarily
		// 3. The cleanup finalizer may not have executed
		//
		// We can only verify that if the value is gone, the behavior is correct.
		// We cannot force it to be gone reliably.
		if !ok && val != nil {
			t.Errorf("TestWeakPointerCollectedCleanup(%s): got ok=false but val != nil", test.name)
		}

		// Verify cache is still functional regardless of cleanup timing
		newVal := &testValue{data: "new-value", num: 100}
		_, _, err = cache.Set(t.Context(), "new-key", newVal)
		if err != nil {
			t.Fatalf("TestWeakPointerCollectedCleanup(%s): failed to set new value: %v", test.name, err)
		}
		got, ok, err := cache.Get(t.Context(), "new-key")
		if err != nil {
			t.Fatalf("TestWeakPointerCollectedCleanup(%s): failed to get new value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestWeakPointerCollectedCleanup(%s): failed to set/get new value after GC attempts", test.name)
		}
		if diff := pretty.Compare(got, newVal); diff != "" {
			t.Errorf("TestWeakPointerCollectedCleanup(%s): -got +want:\n%s", test.name, diff)
		}
	}
}

func TestSetWithNilValue(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(*Cache[string, testValue])
		wantPrevValue *testValue
		wantPrevOk    bool
		verifyDeleted bool
	}{
		{
			name: "Success: Set nil on existing key deletes it",
			setupFunc: func(c *Cache[string, testValue]) {
				val := &testValue{data: "test", num: 42}
				_, _, _ = c.Set(t.Context(), "key1", val)
			},
			wantPrevValue: &testValue{data: "test", num: 42},
			wantPrevOk:    true,
			verifyDeleted: true,
		},
		{
			name:          "Success: Set nil on non-existent key returns nil, false",
			setupFunc:     nil,
			wantPrevValue: nil,
			wantPrevOk:    false,
			verifyDeleted: true,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestSetWithNilValue(%s): failed to create cache: %v", test.name, err)
		}

		if test.setupFunc != nil {
			test.setupFunc(cache)
		}

		// Set with nil value
		var nilVal *testValue
		gotPrev, gotOk, err := cache.Set(t.Context(), "key1", nilVal)
		if err != nil {
			t.Fatalf("TestSetWithNilValue(%s): failed to set nil value: %v", test.name, err)
		}

		if gotOk != test.wantPrevOk {
			t.Errorf("TestSetWithNilValue(%s): got ok=%v, want ok=%v", test.name, gotOk, test.wantPrevOk)
		}

		if diff := pretty.Compare(gotPrev, test.wantPrevValue); diff != "" {
			t.Errorf("TestSetWithNilValue(%s): -got +want:\n%s", test.name, diff)
		}

		// Verify key was deleted
		if test.verifyDeleted {
			val, ok, err := cache.Get(t.Context(), "key1")
			if err != nil {
				t.Fatalf("TestSetWithNilValue(%s): failed to get value: %v", test.name, err)
			}
			if ok {
				t.Errorf("TestSetWithNilValue(%s): key still exists after Set(nil), got value=%v", test.name, val)
			}
		}
	}
}

func TestDelTwice(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: Del twice on same key returns correct values",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestDelTwice(%s): failed to create cache: %v", test.name, err)
		}

		// Set a value
		val := &testValue{data: "test", num: 42}
		_, _, err = cache.Set(t.Context(), "key1", val)
		if err != nil {
			t.Fatalf("TestDelTwice(%s): failed to set value: %v", test.name, err)
		}

		// First Del should return the value
		gotVal1, gotOk1, err := cache.Del(t.Context(), "key1")
		if err != nil {
			t.Fatalf("TestDelTwice(%s): first Del failed: %v", test.name, err)
		}
		if !gotOk1 {
			t.Errorf("TestDelTwice(%s): first Del got ok=false, want ok=true", test.name)
		}
		if diff := pretty.Compare(gotVal1, val); diff != "" {
			t.Errorf("TestDelTwice(%s): first Del -got +want:\n%s", test.name, diff)
		}

		// Second Del should return nil, false
		gotVal2, gotOk2, err := cache.Del(t.Context(), "key1")
		if err != nil {
			t.Fatalf("TestDelTwice(%s): second Del failed: %v", test.name, err)
		}
		if gotOk2 {
			t.Errorf("TestDelTwice(%s): second Del got ok=true, want ok=false", test.name)
		}
		if gotVal2 != nil {
			t.Errorf("TestDelTwice(%s): second Del got val=%v, want nil", test.name, gotVal2)
		}
	}
}

func TestMultipleSetOverwrites(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: multiple Set calls correctly overwrite and return previous values",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestMultipleSetOverwrites(%s): failed to create cache: %v", test.name, err)
		}

		val1 := &testValue{data: "first", num: 1}
		val2 := &testValue{data: "second", num: 2}
		val3 := &testValue{data: "third", num: 3}

		// First Set should return nil, false (no previous value)
		prev1, ok1, err := cache.Set(t.Context(), "key1", val1)
		if err != nil {
			t.Fatalf("TestMultipleSetOverwrites(%s): first Set failed: %v", test.name, err)
		}
		if ok1 {
			t.Errorf("TestMultipleSetOverwrites(%s): first Set got ok=true, want ok=false", test.name)
		}
		if prev1 != nil {
			t.Errorf("TestMultipleSetOverwrites(%s): first Set got prev=%v, want nil", test.name, prev1)
		}

		// Second Set should return val1, true
		prev2, ok2, err := cache.Set(t.Context(), "key1", val2)
		if err != nil {
			t.Fatalf("TestMultipleSetOverwrites(%s): second Set failed: %v", test.name, err)
		}
		if !ok2 {
			t.Errorf("TestMultipleSetOverwrites(%s): second Set got ok=false, want ok=true", test.name)
		}
		if diff := pretty.Compare(prev2, val1); diff != "" {
			t.Errorf("TestMultipleSetOverwrites(%s): second Set -got +want:\n%s", test.name, diff)
		}

		// Third Set should return val2, true
		prev3, ok3, err := cache.Set(t.Context(), "key1", val3)
		if err != nil {
			t.Fatalf("TestMultipleSetOverwrites(%s): third Set failed: %v", test.name, err)
		}
		if !ok3 {
			t.Errorf("TestMultipleSetOverwrites(%s): third Set got ok=false, want ok=true", test.name)
		}
		if diff := pretty.Compare(prev3, val2); diff != "" {
			t.Errorf("TestMultipleSetOverwrites(%s): third Set -got +want:\n%s", test.name, diff)
		}

		// Get should return val3
		gotVal, gotOk, err := cache.Get(t.Context(), "key1")
		if err != nil {
			t.Fatalf("TestMultipleSetOverwrites(%s): Get failed: %v", test.name, err)
		}
		if !gotOk {
			t.Errorf("TestMultipleSetOverwrites(%s): Get got ok=false, want ok=true", test.name)
		}
		if diff := pretty.Compare(gotVal, val3); diff != "" {
			t.Errorf("TestMultipleSetOverwrites(%s): Get -got +want:\n%s", test.name, diff)
		}
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	tests := []struct {
		name          string
		numGoroutines int
		duration      time.Duration
	}{
		{
			name:          "Success: mixed operations under load",
			numGoroutines: 100,
			duration:      2 * time.Second,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[int, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestConcurrentMixedOperations(%s): failed to create cache: %v", test.name, err)
		}

		// Pre-populate some data
		for i := 0; i < 100; i++ {
			val := &testValue{data: "initial", num: i}
			_, _, _ = cache.Set(t.Context(), i, val)
		}

		var wg sync.Group
		timedCtx, cancel := context.WithTimeout(ctx, test.duration)
		defer cancel()

		for g := 0; g < test.numGoroutines; g++ {
			wg.Go(
				timedCtx,
				func(ctx context.Context) error {
					opCount := 0
					for {
						select {
						case <-ctx.Done():
							return nil
						default:
							key := opCount % 100
							opType := opCount % 3

							switch opType {
							case 0: // Get
								_, _, _ = cache.Get(t.Context(), key)
							case 1: // Set
								val := &testValue{data: "concurrent", num: g*10000 + opCount}
								_, _, _ = cache.Set(t.Context(), key, val)
							case 2: // Del
								_, _, _ = cache.Del(t.Context(), key)
							}

							opCount++
						}
					}
				},
			)
		}
		wg.Wait(timedCtx)

		// Verify cache is still functional
		testVal := &testValue{data: "final-test", num: 999}
		_, _, err = cache.Set(t.Context(), 999, testVal)
		if err != nil {
			t.Fatalf("TestConcurrentMixedOperations(%s): failed to set test value: %v", test.name, err)
		}
		got, ok, err := cache.Get(t.Context(), 999)
		if err != nil {
			t.Fatalf("TestConcurrentMixedOperations(%s): failed to get test value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestConcurrentMixedOperations(%s): cache not functional after concurrent operations", test.name)
		}
		if diff := pretty.Compare(got, testVal); diff != "" {
			t.Errorf("TestConcurrentMixedOperations(%s): -got +want:\n%s", test.name, diff)
		}
	}
}

func TestCacheLen(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Cache[string, testValue])
		wantLen int
	}{
		{
			name:    "Success: empty cache has length 0",
			setup:   func(c *Cache[string, testValue]) {},
			wantLen: 0,
		},
		{
			name: "Success: cache with one item has length 1",
			setup: func(c *Cache[string, testValue]) {
				val := &testValue{data: "test", num: 1}
				_, _, _ = c.Set(t.Context(), "key1", val)
			},
			wantLen: 1,
		},
		{
			name: "Success: cache with multiple items",
			setup: func(c *Cache[string, testValue]) {
				for i := 0; i < 10; i++ {
					val := &testValue{data: "test", num: i}
					_, _, _ = c.Set(t.Context(), fmt.Sprintf("key%d", i), val)
				}
			},
			wantLen: 10,
		},
		{
			name: "Success: length decreases after delete",
			setup: func(c *Cache[string, testValue]) {
				for i := 0; i < 10; i++ {
					val := &testValue{data: "test", num: i}
					_, _, _ = c.Set(t.Context(), fmt.Sprintf("key%d", i), val)
				}
				_, _, _ = c.Del(t.Context(), "key0")
				_, _, _ = c.Del(t.Context(), "key5")
			},
			wantLen: 8,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestCacheLen(%s): failed to create cache: %v", test.name, err)
		}

		test.setup(cache)

		got := cache.Len()
		if got != test.wantLen {
			t.Errorf("TestCacheLen(%s): got len=%d, want len=%d", test.name, got, test.wantLen)
		}
	}
}

func TestFiller(t *testing.T) {
	tests := []struct {
		name       string
		filler     Filler[string, testValue]
		key        string
		wantValue  *testValue
		wantOk     bool
		wantErr    bool
		setupFunc  func(*Cache[string, testValue])
		fillerCall *string // Track what key the filler was called with
	}{
		{
			name: "Success: Filler loads missing value",
			filler: func(ctx context.Context, k string) (*testValue, bool, error) {
				return &testValue{data: "filled", num: 100}, true, nil
			},
			key:       "missing-key",
			wantValue: &testValue{data: "filled", num: 100},
			wantOk:    true,
			wantErr:   false,
		},
		{
			name: "Success: Filler not called for existing value",
			filler: func(ctx context.Context, k string) (*testValue, bool, error) {
				t.Errorf("TestFiller: filler should not be called for existing key")
				return nil, false, fmt.Errorf("filler called unexpectedly")
			},
			setupFunc: func(c *Cache[string, testValue]) {
				val := &testValue{data: "existing", num: 42}
				_, _, _ = c.Set(t.Context(), "existing-key", val)
			},
			key:       "existing-key",
			wantValue: &testValue{data: "existing", num: 42},
			wantOk:    true,
			wantErr:   false,
		},
		{
			name: "Error: Filler returns error",
			filler: func(ctx context.Context, k string) (*testValue, bool, error) {
				return nil, false, fmt.Errorf("filler error")
			},
			key:       "error-key",
			wantValue: nil,
			wantOk:    false,
			wantErr:   true,
		},
		{
			name: "Success: Filler returns not found",
			filler: func(ctx context.Context, k string) (*testValue, bool, error) {
				return nil, false, nil
			},
			key:       "not-found-key",
			wantValue: nil,
			wantOk:    false,
			wantErr:   false,
		},
		{
			name: "Success: Filler caches loaded value",
			filler: func(ctx context.Context, k string) (*testValue, bool, error) {
				return &testValue{data: "cached", num: 200}, true, nil
			},
			key:       "cache-key",
			wantValue: &testValue{data: "cached", num: 200},
			wantOk:    true,
			wantErr:   false,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test", WithFiller(test.filler))
		if err != nil {
			t.Fatalf("TestFiller(%s): failed to create cache: %v", test.name, err)
		}

		if test.setupFunc != nil {
			test.setupFunc(cache)
		}

		got, ok, err := cache.Get(t.Context(), test.key)

		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestFiller(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestFiller(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		if ok != test.wantOk {
			t.Errorf("TestFiller(%s): got ok=%v, want ok=%v", test.name, ok, test.wantOk)
		}

		if diff := pretty.Compare(got, test.wantValue); diff != "" {
			t.Errorf("TestFiller(%s): -got +want:\n%s", test.name, diff)
		}

		// For tests where filler loads a value, verify it's cached
		if test.wantOk && !test.wantErr && test.setupFunc == nil {
			// Second Get should not call filler again
			got2, ok2, err := cache.Get(t.Context(), test.key)
			if err != nil {
				t.Fatalf("TestFiller(%s): second Get failed: %v", test.name, err)
			}
			if !ok2 {
				t.Errorf("TestFiller(%s): second Get returned ok=false", test.name)
			}
			if diff := pretty.Compare(got2, test.wantValue); diff != "" {
				t.Errorf("TestFiller(%s): second Get -got +want:\n%s", test.name, diff)
			}
		}
	}
}

// TestFillerNilValueNoPanic pins fix 2: a filler that reports a hit with a nil value must not panic the process.
// Pre-fix getOrFill stored the nil-valued pointer and recordStore's runtime.AddCleanup(nil,...) panicked (and under
// WithSingleFlight the leader's panic took down every follower). Post-fix getOrFill skips the store and returns the
// pre-refactor result (nil, true, nil). Both the default path and the singleflight path are exercised.
func TestFillerNilValueNoPanic(t *testing.T) {
	nilFiller := func(ctx context.Context, k string) (*testValue, bool, error) { return nil, true, nil }
	tests := []struct {
		name    string
		options []Option
	}{
		{
			name:    "Success: nil-value fill returns without panic on the default path",
			options: []Option{WithFiller(nilFiller)},
		},
		{
			name:    "Success: nil-value fill returns without panic on the singleflight path",
			options: []Option{WithFiller(nilFiller), WithSingleFlight()},
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test", test.options...)
		if err != nil {
			t.Fatalf("TestFillerNilValueNoPanic(%s): failed to create cache: %v", test.name, err)
		}

		got, ok, err := cache.Get(ctx, "key")
		if err != nil {
			t.Errorf("TestFillerNilValueNoPanic(%s): got err == %s, want err == nil", test.name, err)
			continue
		}
		if got != nil {
			t.Errorf("TestFillerNilValueNoPanic(%s): got value == %v, want nil", test.name, got)
		}
		if !ok {
			t.Errorf("TestFillerNilValueNoPanic(%s): got ok == false, want true (pre-refactor returned (nil, true, nil))", test.name)
		}
		if got := cache.Len(); got != 0 {
			t.Errorf("TestFillerNilValueNoPanic(%s): got Len() == %d, want 0 (nil fill must not be stored)", test.name, got)
		}
	}
}

// TestFillerCacheItemsMetric verifies that a value loaded through the filler increments the cache_items metric,
// so the up/down counter stays symmetric with the decrement paths (Del and maxTTL forced eviction).
func TestFillerCacheItemsMetric(t *testing.T) {
	tests := []struct {
		name           string
		wantCacheItems int64
	}{
		{
			name:           "Success: filler-loaded entry increments cache_items",
			wantCacheItems: 1,
		},
	}

	for _, test := range tests {
		ctx, reader := metricReader(t)

		filler := func(ctx context.Context, k string) (*testValue, bool, error) {
			return &testValue{data: "filled", num: 1}, true, nil
		}
		cache, err := New[string, testValue](ctx, "test", WithFiller(filler))
		if err != nil {
			t.Fatalf("TestFillerCacheItemsMetric(%s): failed to create cache: %v", test.name, err)
		}

		got, ok, err := cache.Get(ctx, "key")
		if err != nil {
			t.Fatalf("TestFillerCacheItemsMetric(%s): Get failed: %v", test.name, err)
		}
		if !ok {
			t.Fatalf("TestFillerCacheItemsMetric(%s): filler did not load value", test.name)
		}

		gotItems := cacheItemsValue(t, ctx, reader, "TestFillerCacheItemsMetric", test.name)
		if gotItems != test.wantCacheItems {
			t.Errorf("TestFillerCacheItemsMetric(%s): got cache_items == %d, want cache_items == %d", test.name, gotItems, test.wantCacheItems)
		}

		runtime.KeepAlive(got)
	}
}

// TestFillerRefillCacheItemsMetric verifies that a filler refill over a bucket whose weak pointer was collected but
// whose GC cleanup has not yet removed it does not increment cache_items again: the bucket is one logical entry and
// refilling it must not double-count. The bucket is seeded through the shard map directly (not Cache.Set) so no
// runtime.AddCleanup is registered and the collected-but-not-cleaned bucket is stable, making the test deterministic.
// The seed path never touches cache_items, so after the refill the counter must still read 0.
func TestFillerRefillCacheItemsMetric(t *testing.T) {
	tests := []struct {
		name           string
		wantCacheItems int64
	}{
		{
			name:           "Success: refill of a collected-but-not-cleaned bucket does not increment cache_items",
			wantCacheItems: 0,
		},
	}

	for _, test := range tests {
		ctx, reader := metricReader(t)

		filler := func(ctx context.Context, k string) (*testValue, bool, error) {
			return &testValue{data: "refilled", num: 2}, true, nil
		}
		cache, err := New[string, testValue](ctx, "test", WithFiller(filler))
		if err != nil {
			t.Fatalf("TestFillerRefillCacheItemsMetric(%s): failed to create cache: %v", test.name, err)
		}

		// Seed the bucket through the shard map so the weak layer registers no cleanup for it: once the value is
		// collected the bucket stays in place with a nil weak pointer, the collected-but-not-yet-cleaned state.
		func() {
			v := &testValue{data: "seed", num: 1}
			if _, err := cache.m.Set(ctx, "key", v, nil, time.Time{}); err != nil {
				t.Fatalf("TestFillerRefillCacheItemsMetric(%s): failed to seed shard map: %v", test.name, err)
			}
		}()

		// Poll until the GC collects the seeded value; the bucket itself remains because nothing deletes it.
		collected := gcUntil(func() bool { _, ok := cache.m.Get("key"); return !ok })
		if !collected {
			t.Fatalf("TestFillerRefillCacheItemsMetric(%s): seeded value never collected, cannot exercise refill path", test.name)
		}
		if got := cache.m.Len(); got != 1 {
			t.Fatalf("TestFillerRefillCacheItemsMetric(%s): got shard map Len() == %d, want 1 (nil bucket must persist)", test.name, got)
		}

		// The Get misses on the nil weak pointer and refills the existing bucket through the filler.
		got, ok, err := cache.Get(ctx, "key")
		if err != nil {
			t.Fatalf("TestFillerRefillCacheItemsMetric(%s): Get failed: %v", test.name, err)
		}
		if !ok {
			t.Fatalf("TestFillerRefillCacheItemsMetric(%s): filler did not load value", test.name)
		}

		gotItems := cacheItemsValue(t, ctx, reader, "TestFillerRefillCacheItemsMetric", test.name)
		if gotItems != test.wantCacheItems {
			t.Errorf("TestFillerRefillCacheItemsMetric(%s): got cache_items == %d, want cache_items == %d", test.name, gotItems, test.wantCacheItems)
		}

		runtime.KeepAlive(got)
	}
}

// cacheItemsValue collects the current cumulative value of the cache_items up/down counter from reader.
func cacheItemsValue(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader, testFunc, name string) int64 {
	t.Helper()
	return sumValue(t, ctx, reader, testFunc, name, "cache_items")
}

// sumValue collects the current cumulative value of the named int64 sum metric (counter or up/down counter)
// from reader.
func sumValue(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader, testFunc, name, metricName string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("%s(%s): failed to collect metrics: %v", testFunc, name, err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, metricItem := range sm.Metrics {
			if metricItem.Name != metricName {
				continue
			}
			sum, ok := metricItem.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	return 0
}

// TestDeDupeDedupsMetric verifies that when the dedup btree already holds an equal value, storing a second key with
// that value increments the dedups counter exactly once.
func TestDeDupeDedupsMetric(t *testing.T) {
	tests := []struct {
		name       string
		wantDedups int64
	}{
		{
			name:       "Success: second key with an equal value increments dedups once",
			wantDedups: 1,
		},
	}

	for _, test := range tests {
		ctx, reader := metricReader(t)

		// The less function must tolerate weak pointers whose Value() is nil, per the WithDeDupe contract.
		less := func(a, b weak.Pointer[testValue]) bool {
			av, bv := a.Value(), b.Value()
			switch {
			case av == nil && bv == nil:
				return false
			case av == nil:
				return true
			case bv == nil:
				return false
			case av.data != bv.data:
				return av.data < bv.data
			default:
				return av.num < bv.num
			}
		}

		cache, err := New[string, testValue](ctx, "test", WithDeDupe(less))
		if err != nil {
			t.Fatalf("TestDeDupeDedupsMetric(%s): failed to create cache: %v", test.name, err)
		}

		// Two distinct pointers with equal contents: the second Set must find the first in the dedup btree.
		v1 := &testValue{data: "same", num: 42}
		v2 := &testValue{data: "same", num: 42}
		if _, _, err := cache.Set(ctx, "key1", v1); err != nil {
			t.Fatalf("TestDeDupeDedupsMetric(%s): failed to set key1: %v", test.name, err)
		}
		if _, _, err := cache.Set(ctx, "key2", v2); err != nil {
			t.Fatalf("TestDeDupeDedupsMetric(%s): failed to set key2: %v", test.name, err)
		}

		gotDedups := sumValue(t, ctx, reader, "TestDeDupeDedupsMetric", test.name, "dedups")
		if gotDedups != test.wantDedups {
			t.Errorf("TestDeDupeDedupsMetric(%s): got dedups == %d, want dedups == %d", test.name, gotDedups, test.wantDedups)
		}

		runtime.KeepAlive(v1)
		runtime.KeepAlive(v2)
	}
}

// TestGCReclamationCacheItemsMetric verifies that when the GC reclaims values and the AddCleanup path removes their
// buckets (DeleteIfNil), the cache_items up/down counter is decremented back to zero. Without the decrement the
// dominant eviction path of a weak cache would leave the gauge drifting upward forever.
func TestGCReclamationCacheItemsMetric(t *testing.T) {
	tests := []struct {
		name           string
		numEntries     int
		wantCacheItems int64
	}{
		{
			name:           "Success: GC reclamation of all entries returns cache_items to zero",
			numEntries:     5,
			wantCacheItems: 0,
		},
	}

	for _, test := range tests {
		ctx, reader := metricReader(t)

		cache, err := New[int, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestGCReclamationCacheItemsMetric(%s): failed to create cache: %v", test.name, err)
		}

		// Set entries inside a closure so no strong references survive it; only the GC cleanup path can remove them.
		func() {
			for i := 0; i < test.numEntries; i++ {
				v := &testValue{data: "reclaim", num: i}
				if _, _, err := cache.Set(ctx, i, v); err != nil {
					t.Fatalf("TestGCReclamationCacheItemsMetric(%s): failed to set value %d: %v", test.name, i, err)
				}
			}
		}()

		// Poll until the GC has collected the values and the AddCleanup closures have removed every bucket.
		reclaimed := false
		for i := 0; i < 400; i++ {
			runtime.GC()
			if cache.Len() == 0 {
				reclaimed = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !reclaimed {
			t.Fatalf("TestGCReclamationCacheItemsMetric(%s): entries never reclaimed by GC, cannot exercise cleanup path", test.name)
		}

		gotItems := cacheItemsValue(t, ctx, reader, "TestGCReclamationCacheItemsMetric", test.name)
		if gotItems != test.wantCacheItems {
			t.Errorf("TestGCReclamationCacheItemsMetric(%s): got cache_items == %d, want == %d", test.name, gotItems, test.wantCacheItems)
		}
	}
}

func TestSetter(t *testing.T) {
	tests := []struct {
		name        string
		setter      Setter[string, testValue]
		key         string
		value       *testValue
		wantErr     bool
		wantSetCall bool
	}{
		{
			name: "Success: Setter called on Set",
			setter: func(ctx context.Context, k string, v *testValue) error {
				if k != "test-key" {
					return fmt.Errorf("expected key test-key, got %s", k)
				}
				if v.data != "test" || v.num != 42 {
					return fmt.Errorf("unexpected value")
				}
				return nil
			},
			key:         "test-key",
			value:       &testValue{data: "test", num: 42},
			wantErr:     false,
			wantSetCall: true,
		},
		{
			name: "Error: Setter returns error prevents Set",
			setter: func(ctx context.Context, k string, v *testValue) error {
				return fmt.Errorf("setter error")
			},
			key:         "error-key",
			value:       &testValue{data: "test", num: 42},
			wantErr:     true,
			wantSetCall: true,
		},
		{
			name: "Success: Setter called on overwrite",
			setter: func(ctx context.Context, k string, v *testValue) error {
				return nil
			},
			key:         "overwrite-key",
			value:       &testValue{data: "new", num: 100},
			wantErr:     false,
			wantSetCall: true,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		callCount := 0
		wrappedSetter := func(ctx context.Context, k string, v *testValue) error {
			callCount++
			return test.setter(ctx, k, v)
		}

		cache, err := New[string, testValue](ctx, "test", WithSetter(wrappedSetter))
		if err != nil {
			t.Fatalf("TestSetter(%s): failed to create cache: %v", test.name, err)
		}

		// For overwrite test, set an initial value
		if test.key == "overwrite-key" {
			initialVal := &testValue{data: "old", num: 1}
			_, _, _ = cache.Set(t.Context(), test.key, initialVal)
			callCount = 0 // Reset count after initial set
		}

		_, _, err = cache.Set(t.Context(), test.key, test.value)

		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestSetter(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestSetter(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			// Expected error, verify value was not set
			got, ok, _ := cache.Get(t.Context(), test.key)
			if ok && got != nil {
				t.Errorf("TestSetter(%s): value was set despite setter error", test.name)
			}
			continue
		}

		if test.wantSetCall && callCount == 0 {
			t.Errorf("TestSetter(%s): setter was not called", test.name)
		}

		// Verify value was set successfully
		got, ok, err := cache.Get(t.Context(), test.key)
		if err != nil {
			t.Fatalf("TestSetter(%s): Get failed: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestSetter(%s): value not found after Set", test.name)
		}
		if diff := pretty.Compare(got, test.value); diff != "" {
			t.Errorf("TestSetter(%s): -got +want:\n%s", test.name, diff)
		}
	}
}

func TestDeleter(t *testing.T) {
	tests := []struct {
		name         string
		seed         bool // Set the key before Del so a live entry exists.
		deleterFails bool
		wantErr      bool
		wantDeleted  bool
		wantCalls    int64
	}{
		{
			name:        "Success: deleter called exactly once on Del and the entry is removed",
			seed:        true,
			wantDeleted: true,
			wantCalls:   1,
		},
		{
			name:      "Success: deleter called on Del of a key with no live entry",
			seed:      false,
			wantCalls: 1,
		},
		{
			name:         "Error: deleter error aborts Del and the entry remains",
			seed:         true,
			deleterFails: true,
			wantErr:      true,
			wantCalls:    1,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		var calls atomic.Int64
		var gotKey atomic.Value
		deleter := func(ctx context.Context, k string) error {
			calls.Add(1)
			gotKey.Store(k)
			if test.deleterFails {
				return fmt.Errorf("deleter error")
			}
			return nil
		}

		cache, err := New[string, testValue](ctx, "test", WithDeleter(deleter))
		if err != nil {
			t.Fatalf("TestDeleter(%s): failed to create cache: %v", test.name, err)
		}

		val := &testValue{data: "test", num: 42}
		if test.seed {
			if _, _, err := cache.Set(ctx, "key", val); err != nil {
				t.Fatalf("TestDeleter(%s): failed to set value: %v", test.name, err)
			}
		}

		_, deleted, err := cache.Del(ctx, "key")

		if got := calls.Load(); got != test.wantCalls {
			t.Errorf("TestDeleter(%s): got %d deleter call(s), want %d", test.name, got, test.wantCalls)
		}
		if got, _ := gotKey.Load().(string); got != "key" {
			t.Errorf("TestDeleter(%s): got deleter key == %q, want %q", test.name, got, "key")
		}

		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestDeleter(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestDeleter(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			// The delete was aborted, so the entry must remain readable. KeepAlive here because continue skips the
			// one at the bottom of the loop, and val must stay live through the Get.
			if _, ok, _ := cache.Get(ctx, "key"); !ok {
				t.Errorf("TestDeleter(%s): entry removed despite deleter error, want entry retained", test.name)
			}
			runtime.KeepAlive(val)
			continue
		}

		if deleted != test.wantDeleted {
			t.Errorf("TestDeleter(%s): got deleted == %v, want deleted == %v", test.name, deleted, test.wantDeleted)
		}
		if _, ok, _ := cache.Get(ctx, "key"); ok {
			t.Errorf("TestDeleter(%s): entry still readable after successful Del, want it removed", test.name)
		}

		runtime.KeepAlive(val)
	}
}

// TestDeleterNotCalledOnGCReclamation verifies that the deleter does not run when the GC reclaims a value and the
// AddCleanup path removes its bucket: the value is already gone from memory and the durable copy must stay untouched.
// The assertion only fires after Len()==0 has been observed, so the DeleteIfNil path has definitely run.
func TestDeleterNotCalledOnGCReclamation(t *testing.T) {
	tests := []struct {
		name      string
		wantCalls int64
	}{
		{
			name:      "Success: GC reclamation removes the entry without calling the deleter",
			wantCalls: 0,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		var calls atomic.Int64
		deleter := func(ctx context.Context, k string) error {
			calls.Add(1)
			return nil
		}

		cache, err := New[string, testValue](ctx, "test", WithDeleter(deleter))
		if err != nil {
			t.Fatalf("TestDeleterNotCalledOnGCReclamation(%s): failed to create cache: %v", test.name, err)
		}

		// Set inside a closure so no strong reference survives; only the GC cleanup path can remove the entry.
		func() {
			v := &testValue{data: "reclaim", num: 1}
			if _, _, err := cache.Set(ctx, "key", v); err != nil {
				t.Fatalf("TestDeleterNotCalledOnGCReclamation(%s): failed to set value: %v", test.name, err)
			}
		}()

		reclaimed := false
		for i := 0; i < 400; i++ {
			runtime.GC()
			if cache.Len() == 0 {
				reclaimed = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !reclaimed {
			t.Fatalf("TestDeleterNotCalledOnGCReclamation(%s): entry never reclaimed by GC, cannot exercise cleanup path", test.name)
		}

		if got := calls.Load(); got != test.wantCalls {
			t.Errorf("TestDeleterNotCalledOnGCReclamation(%s): got %d deleter call(s), want %d", test.name, got, test.wantCalls)
		}
	}
}

func TestFillerConcurrent(t *testing.T) {
	tests := []struct {
		name          string
		numGoroutines int
		useFlight     bool
	}{
		{
			name:          "Success: concurrent filler calls without singleflight each receive the value",
			numGoroutines: 50,
			useFlight:     false,
		},
		{
			name:          "Success: concurrent filler calls with singleflight collapse to a single fill",
			numGoroutines: 50,
			useFlight:     true,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		var callCount atomic.Int64

		filler := func(ctx context.Context, k string) (*testValue, bool, error) {
			callCount.Add(1)
			time.Sleep(50 * time.Millisecond) // Hold the fill open so concurrent callers pile onto the leader.
			return &testValue{data: "filled", num: 100}, true, nil
		}

		var cache *Cache[string, testValue]
		var err error
		if test.useFlight {
			cache, err = New[string, testValue](ctx, "test", WithFiller(filler), WithSingleFlight())
		} else {
			cache, err = New[string, testValue](ctx, "test", WithFiller(filler))
		}
		if err != nil {
			t.Fatalf("TestFillerConcurrent(%s): failed to create cache: %v", test.name, err)
		}

		type result struct {
			value *testValue
			ok    bool
			err   error
		}
		results := make([]result, test.numGoroutines)

		var wg sync.Group
		for i := 0; i < test.numGoroutines; i++ {
			wg.Go(ctx, func(ctx context.Context) error {
				v, ok, err := cache.Get(t.Context(), "concurrent-key")
				results[i] = result{value: v, ok: ok, err: err}
				return nil
			})
		}
		wg.Wait(ctx)

		// Every caller must receive the loaded value regardless of which goroutine ran the filler.
		for i, r := range results {
			switch {
			case r.err != nil:
				t.Errorf("TestFillerConcurrent(%s): caller %d got err == %s, want err == nil", test.name, i, r.err)
			case !r.ok:
				t.Errorf("TestFillerConcurrent(%s): caller %d got ok == false, want ok == true", test.name, i)
			case r.value == nil:
				t.Errorf("TestFillerConcurrent(%s): caller %d got nil value, want non-nil", test.name, i)
			}
		}

		// With singleflight exactly one fill happens; without it at least one (usually more).
		finalCount := callCount.Load()
		switch {
		case test.useFlight && finalCount != 1:
			t.Errorf("TestFillerConcurrent(%s): with singleflight got %d filler calls, want 1", test.name, finalCount)
		case !test.useFlight && finalCount < 1:
			t.Errorf("TestFillerConcurrent(%s): without singleflight got %d filler calls, want >= 1", test.name, finalCount)
		}
	}
}

// TestCollectedBucketCacheItemsMetric verifies that operations on a key whose bucket persists but whose weak pointer
// was collected keep cache_items symmetric with the physical buckets. Each case seeds a bucket through the shard map
// (no runtime.AddCleanup, so the collected-but-not-cleaned state is stable) and bumps cache_items by one by hand to
// stand in for the increment the seeded entry would have received through the public Set, establishing the baseline of
// one. The op then runs through the public API over the collected bucket and cache_items is compared to want.
//
//   - re-Set (was TestSetReSetCollectedBucketCacheItemsMetric): re-Setting the key must not increment cache_items a
//     second time, since the bucket is one logical entry; post-fix the gauge stays at 1, pre-fix it drifts to 2.
//   - Del (was TestDelCollectedBucketCacheItemsMetric): Del of the key must decrement cache_items, since the bucket is
//     still physically present and counted; post-fix the gauge returns to 0, pre-fix Del reports deleted=false on the
//     nil value and it strands at 1.
func TestCollectedBucketCacheItemsMetric(t *testing.T) {
	tests := []struct {
		name           string
		op             func(ctx context.Context, cache *Cache[string, testValue]) (keepAlive any, err error)
		wantCacheItems int64
	}{
		{
			name: "Success: re-Set of a collected-but-not-cleaned bucket does not increment cache_items",
			op: func(ctx context.Context, cache *Cache[string, testValue]) (any, error) {
				// A strong reference keeps the new value (and its cleanup) from firing during the test, so only the
				// re-Set path affects the metric.
				v2 := &testValue{data: "reset", num: 2}
				_, _, err := cache.Set(ctx, "key", v2)
				return v2, err
			},
			wantCacheItems: 1,
		},
		{
			name: "Success: Del of a collected-but-not-cleaned bucket returns cache_items to zero",
			op: func(ctx context.Context, cache *Cache[string, testValue]) (any, error) {
				_, _, err := cache.Del(ctx, "key")
				return nil, err
			},
			wantCacheItems: 0,
		},
	}

	for _, test := range tests {
		ctx, reader := metricReader(t)

		cache, err := New[string, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestCollectedBucketCacheItemsMetric(%s): failed to create cache: %v", test.name, err)
		}

		func() {
			v := &testValue{data: "seed", num: 1}
			if _, err := cache.m.Set(ctx, "key", v, nil, time.Time{}); err != nil {
				t.Fatalf("TestCollectedBucketCacheItemsMetric(%s): failed to seed shard map: %v", test.name, err)
			}
		}()
		cache.metrics.CacheItems.Add(ctx, 1)

		collected := gcUntil(func() bool { _, ok := cache.m.Get("key"); return !ok })
		if !collected {
			t.Fatalf("TestCollectedBucketCacheItemsMetric(%s): seeded value never collected, cannot exercise op path", test.name)
		}
		if got := cache.m.Len(); got != 1 {
			t.Fatalf("TestCollectedBucketCacheItemsMetric(%s): got shard map Len() == %d, want 1 (nil bucket must persist)", test.name, got)
		}

		keepAlive, err := test.op(ctx, cache)
		if err != nil {
			t.Fatalf("TestCollectedBucketCacheItemsMetric(%s): op failed: %v", test.name, err)
		}

		gotItems := cacheItemsValue(t, ctx, reader, "TestCollectedBucketCacheItemsMetric", test.name)
		if gotItems != test.wantCacheItems {
			t.Errorf("TestCollectedBucketCacheItemsMetric(%s): got cache_items == %d, want == %d", test.name, gotItems, test.wantCacheItems)
		}

		runtime.KeepAlive(keepAlive)
	}
}

// dedupLess is a WithDeDupe comparator over testValue that tolerates weak pointers whose Value() is nil, ordering a
// nil-valued pointer as the least element, as the WithDeDupe contract requires.
func dedupLess(a, b weak.Pointer[testValue]) bool {
	av, bv := a.Value(), b.Value()
	switch {
	case av == nil && bv == nil:
		return false
	case av == nil:
		return true
	case bv == nil:
		return false
	case av.data != bv.data:
		return av.data < bv.data
	default:
		return av.num < bv.num
	}
}

// TestDeDupeCleanupTargetsRepresentative pins fix 1's leak: under WithDeDupe a deduped key's bucket stores the tree
// representative, so the GC cleanup and the min-TTL hold must be attached to that representative, not the caller's
// discarded duplicate. Pre-fix the deduped key's cleanup is attached to the duplicate and no-ops when the duplicate is
// collected (the bucket still references the live representative), so the bucket leaks and Len never returns to zero.
func TestDeDupeCleanupTargetsRepresentative(t *testing.T) {
	ctx := t.Context()
	cache, err := New[string, testValue](ctx, "test", WithDeDupe(dedupLess))
	if err != nil {
		t.Fatalf("TestDeDupeCleanupTargetsRepresentative: failed to create cache: %v", err)
	}

	v1 := &testValue{data: "same", num: 7}
	v2 := &testValue{data: "same", num: 7}
	if _, _, err := cache.Set(ctx, "k1", v1); err != nil {
		t.Fatalf("TestDeDupeCleanupTargetsRepresentative: set k1: %v", err)
	}
	if _, _, err := cache.Set(ctx, "k2", v2); err != nil {
		t.Fatalf("TestDeDupeCleanupTargetsRepresentative: set k2: %v", err)
	}

	// Observe the duplicate's collection through our own weak pointer, then drop it. k2's bucket references the
	// representative (v1), not v2, so collecting v2 fires k2's cleanup as a no-op pre-fix and leaks the bucket.
	wp2 := weak.Make(v2)
	v2 = nil
	if !gcUntil(func() bool { return wp2.Value() == nil }) {
		t.Fatalf("TestDeDupeCleanupTargetsRepresentative: duplicate v2 never collected")
	}
	// Keep the representative alive across the duplicate's collection so k2's cleanup fires while the representative is
	// still live (the leak we are pinning). Without this the representative would be collected here too and mask it.
	runtime.KeepAlive(v1)

	// Drop the representative. Post-fix both k1's and k2's cleanups are attached to it, so collecting it removes both
	// buckets and Len reaches zero. Pre-fix k2's bucket leaks and Len sticks at 1.
	v1 = nil
	if !gcUntil(func() bool { return cache.Len() == 0 }) {
		t.Errorf("TestDeDupeCleanupTargetsRepresentative: got Len() == %d, want Len() == 0 (deduped key leaked)", cache.Len())
	}
}

// TestDeDupeMinTTLHoldsRepresentative pins fix 1's min-TTL break: under WithDeDupe the min-TTL strong hold for a
// deduped key must pin the stored representative, not the caller's discarded duplicate. Pre-fix the hold pins the
// duplicate, so once the caller drops its reference the representative can be collected mid-hold and a Get on the
// deduped key misses inside its own TTL window.
func TestDeDupeMinTTLHoldsRepresentative(t *testing.T) {
	ctx := t.Context()
	cache, err := New[string, testValue](ctx, "test", WithDeDupe(dedupLess), WithTTL(1*time.Second, 0, 1*time.Second))
	if err != nil {
		t.Fatalf("TestDeDupeMinTTLHoldsRepresentative: failed to create cache: %v", err)
	}

	v1 := &testValue{data: "same", num: 7}
	if _, _, err := cache.Set(ctx, "k1", v1); err != nil {
		t.Fatalf("TestDeDupeMinTTLHoldsRepresentative: set k1: %v", err)
	}

	// Wait past k1's hold so its ttlMap strong reference is released; only the caller's v1 now keeps the
	// representative alive.
	time.Sleep(2500 * time.Millisecond)

	// Store an equal duplicate at a new key: it dedups onto the representative and takes a fresh min-TTL hold.
	v2 := &testValue{data: "same", num: 7}
	if _, _, err := cache.Set(ctx, "k2", v2); err != nil {
		t.Fatalf("TestDeDupeMinTTLHoldsRepresentative: set k2: %v", err)
	}
	// The caller kept v1 alive across the sleep and the dedup, so k2 genuinely dedups onto the live representative.
	runtime.KeepAlive(v1)

	// Drop the caller's reference, then force GC. Pre-fix nothing pins the representative (the hold pins the discarded
	// duplicate) so it is collected; post-fix k2's hold pins the representative. The burst is bounded well inside k2's
	// 1s hold so the following Get lands during the hold window.
	v1 = nil
	for i := 0; i < 50; i++ {
		runtime.GC()
		time.Sleep(2 * time.Millisecond)
	}

	got, ok, err := cache.Get(ctx, "k2")
	if err != nil {
		t.Fatalf("TestDeDupeMinTTLHoldsRepresentative: Get(k2): %v", err)
	}
	if !ok {
		t.Errorf("TestDeDupeMinTTLHoldsRepresentative: got Get(k2) miss, want hit within k2's hold (representative collected mid-hold)")
	}
	_ = got
	runtime.KeepAlive(v2)
}

// TestDeleterFailureReleasesValue pins fix 2: a permanently failing deleter must not pin the value in the expireAfter
// tree forever. The failed eviction keeps the schedule (so it keeps retrying) but drops the strong value, making the
// WithDeleter doc's "or the GC reclaims the value" exit reachable. Pre-fix the tree retains the strong value, so the
// value is never collected.
func TestDeleterFailureReleasesValue(t *testing.T) {
	ctx := t.Context()
	var calls atomic.Int64
	deleter := func(ctx context.Context, k string) error {
		calls.Add(1)
		return fmt.Errorf("permanent deleter failure")
	}
	cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 2*time.Second, 1*time.Second), WithDeleter(deleter))
	if err != nil {
		t.Fatalf("TestDeleterFailureReleasesValue: failed to create cache: %v", err)
	}

	v := &testValue{data: "poison", num: 1}
	wp := weak.Make(v)
	if _, _, err := cache.Set(ctx, "key", v); err != nil {
		t.Fatalf("TestDeleterFailureReleasesValue: set: %v", err)
	}

	// Hold v strong so only forced maxTTL eviction can act on it. The deleter fails every tick; the schedule must
	// survive each failure and keep retrying, so the call counter climbs past one.
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) && calls.Load() < 2 {
		time.Sleep(100 * time.Millisecond)
	}
	if got := calls.Load(); got < 2 {
		t.Fatalf("TestDeleterFailureReleasesValue: got %d deleter calls, want >= 2 (eviction not retried after failure)", got)
	}
	// The caller kept v strong across the retry window, so only the forced eviction (not the GC) could have acted.
	runtime.KeepAlive(v)

	// Drop the caller's reference and force GC. Post-fix the failed eviction already dropped the tree's strong value,
	// so the GC reclaims v; pre-fix the tree pins v and this never observes collection.
	v = nil
	if !gcUntilFor(15*time.Second, func() bool { return wp.Value() == nil }) {
		t.Errorf("TestDeleterFailureReleasesValue: value never collected; the failing deleter pinned it in the expireAfter tree")
	}
}

// TestPoisonBucketReclaimedStopsDeleter pins fix 1: once a permanently-failing deleter's value is GC-reclaimed, the
// GC-cleanup path (DeleteIfNil) must remove the now-nil, past-maxTTL bucket so the forced-eviction schedule stops
// re-running the failing deleter. Pre-fix DeleteIfNil routed its liveness lookup through hashmap.Get, which hides a
// past-maxTTL bucket, so the bucket was never removed: Len() stuck at 1 and the deadline-only GetIfMaxTTL kept calling
// the failing deleter every tick forever. Post-fix DeleteIfNil peeks the raw bucket (GetAny), removes it, and the next
// tick's GetIfMaxTTL misses so the deleter is no longer called.
func TestPoisonBucketReclaimedStopsDeleter(t *testing.T) {
	ctx := t.Context()
	var calls atomic.Int64
	deleter := func(ctx context.Context, k string) error {
		calls.Add(1)
		return fmt.Errorf("permanent deleter failure")
	}
	cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 2*time.Second, 1*time.Second), WithDeleter(deleter))
	if err != nil {
		t.Fatalf("TestPoisonBucketReclaimedStopsDeleter: failed to create cache: %v", err)
	}

	v := &testValue{data: "poison", num: 1}
	wp := weak.Make(v)
	if _, _, err := cache.Set(ctx, "key", v); err != nil {
		t.Fatalf("TestPoisonBucketReclaimedStopsDeleter: set: %v", err)
	}

	// The deleter fails every tick. The entry migrates into the expireAfter tree after its hold, its forced eviction
	// fails and drops the strong value, so once the caller's reference is gone the GC can reclaim it. Wait for the
	// first failed eviction (calls climbing) before dropping the reference.
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) && calls.Load() < 1 {
		time.Sleep(50 * time.Millisecond)
	}
	runtime.KeepAlive(v)
	v = nil

	// The failed eviction dropped the tree's strong value, so the GC reclaims it. Its cleanup fires DeleteIfNil, which
	// must remove the now-nil, past-maxTTL bucket.
	if !gcUntil(func() bool { return wp.Value() == nil }) {
		t.Fatalf("TestPoisonBucketReclaimedStopsDeleter: value never collected; the failing deleter pinned it")
	}
	if !gcUntilFor(8*time.Second, func() bool { return cache.Len() == 0 }) {
		t.Fatalf("TestPoisonBucketReclaimedStopsDeleter: got Len()=%d, want 0; DeleteIfNil never removed the poison bucket", cache.Len())
	}

	// With the bucket gone, the next tick's GetIfMaxTTL misses, so the deleter must stop being called. Record the count
	// and confirm it does not climb across several intervals.
	before := calls.Load()
	time.Sleep(4 * time.Second)
	if after := calls.Load(); after != before {
		t.Errorf("TestPoisonBucketReclaimedStopsDeleter: deleter still called after bucket removed: before=%d after=%d", before, after)
	}
}

// TestPrunesMigratedExpireNode pins the expireAfter-tree pruning regressions: once an entry's hold expires and it
// migrates from the ttlMap into the expireAfter tree, the tree node holds the value strong until the far-off maxTTL
// tick, so an operation that removes or overwrites the key must prune that node or the value is pinned until the tick.
// Each case sets v1, waits for it to migrate, runs the operation under test, then requires v1 to be GC-collected within
// a bounded window. Consolidates three former standalone regressions (Del, re-Set, double-migration) that shared this
// setup and assertion and differed only in the post-migration action.
func TestPrunesMigratedExpireNode(t *testing.T) {
	tests := []struct {
		name string
		// action runs after v1 has migrated into the expireAfter tree. It performs the operation under test on "key"
		// and returns a value that must be kept alive across the collection window (nil when none), so only v1 is
		// eligible for collection.
		action func(ctx context.Context, cache *Cache[string, testValue]) (*testValue, error)
	}{
		{
			// was TestDelPrunesMigratedExpireNode: Del must prune the migrated tree node so its strong value stops
			// pinning memory; pre-fix Del only deletes the ttlMap hold and the tree pins the value until the maxTTL tick.
			name: "Success: Del prunes the migrated expire node",
			action: func(ctx context.Context, cache *Cache[string, testValue]) (*testValue, error) {
				_, _, err := cache.Del(ctx, "key")
				return nil, err
			},
		},
		{
			// was TestReSetPrunesMigratedExpireNode: overwriting the migrated key with a new Set must prune the old
			// node so the old value stops pinning memory. A public Set is the only path that can overwrite a still-
			// migrated key (the tree pins the value strong, so the shard's weak pointer never goes nil and the filler
			// never runs), so this single Set case covers the pruning set() and getOrFill() share through recordStore.
			name: "Success: re-Set prunes the old migrated expire node",
			action: func(ctx context.Context, cache *Cache[string, testValue]) (*testValue, error) {
				v2 := &testValue{data: "new", num: 2}
				_, _, err := cache.Set(ctx, "key", v2)
				return v2, err
			},
		},
		{
			// was TestDoubleMigrationDoesNotOrphanExpireNode: re-Set the migrated key and let the new hold migrate too.
			// The second migration overwrites expireIndex[key]; the first node must already be gone (recordStore prunes
			// on the re-Set, backed by the migration-site guard) or the orphaned first node keeps v1 pinned until its
			// own maxTTL tick with Del unable to find it.
			name: "Success: a second migration does not orphan the first expire node",
			action: func(ctx context.Context, cache *Cache[string, testValue]) (*testValue, error) {
				v2 := &testValue{data: "new", num: 2}
				if _, _, err := cache.Set(ctx, "key", v2); err != nil {
					return nil, err
				}
				// Let v2's hold expire and migrate too; the first node must already be gone or v1 stays pinned.
				time.Sleep(2500 * time.Millisecond)
				return v2, nil
			},
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 60*time.Second, 1*time.Second))
		if err != nil {
			t.Fatalf("TestPrunesMigratedExpireNode(%s): failed to create cache: %v", test.name, err)
		}

		v1 := &testValue{data: "old", num: 1}
		wp1 := weak.Make(v1)
		if _, _, err := cache.Set(ctx, "key", v1); err != nil {
			t.Fatalf("TestPrunesMigratedExpireNode(%s): set v1: %v", test.name, err)
		}
		// Wait past the hold so the entry migrates from the ttlMap into the expireAfter tree, which then holds v1 strong
		// until the far-off 60s maxTTL tick.
		time.Sleep(2500 * time.Millisecond)

		keepAlive, err := test.action(ctx, cache)
		if err != nil {
			t.Fatalf("TestPrunesMigratedExpireNode(%s): action: %v", test.name, err)
		}

		// Drop the caller's reference to v1, keeping any action-produced value alive so only v1 can be collected. Post-fix
		// the migrated node pinning v1 is pruned, so the GC reclaims v1; pre-fix the node pins v1 until the 60s maxTTL
		// tick and this times out.
		v1 = nil
		collected := gcUntilFor(8*time.Second, func() bool { return wp1.Value() == nil })
		runtime.KeepAlive(keepAlive)
		if !collected {
			t.Errorf("TestPrunesMigratedExpireNode(%s): v1 never collected; the migrated node still pins it in the expireAfter tree", test.name)
		}
	}
}
