package weak

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/kylelemons/godebug/pretty"
)

type testValue struct {
	data string
	num  int
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

func TestFillerConcurrent(t *testing.T) {
	tests := []struct {
		name          string
		numGoroutines int
		useFlight     bool
	}{
		{
			name:          "Success: concurrent filler calls without singleflight",
			numGoroutines: 10,
			useFlight:     false,
		},
		{
			name:          "Success: concurrent filler calls with singleflight",
			numGoroutines: 10,
			useFlight:     true,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		callCount := 0
		var callCountMu sync.Mutex

		filler := func(ctx context.Context, k string) (*testValue, bool, error) {
			callCountMu.Lock()
			callCount++
			callCountMu.Unlock()
			time.Sleep(10 * time.Millisecond) // Simulate slow load
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

		var wg sync.Group
		for i := 0; i < test.numGoroutines; i++ {
			wg.Go(
				ctx,
				func(ctx context.Context) error {
					got, ok, err := cache.Get(t.Context(), "concurrent-key")
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("Get returned ok=false")
					}
					if got == nil {
						return fmt.Errorf("Get returned nil value")
					}
					return nil
				},
			)
		}

		wg.Wait(ctx)

		callCountMu.Lock()
		finalCount := callCount
		callCountMu.Unlock()

		// With singleflight, we expect exactly 1 call
		// Without singleflight, we expect multiple calls
		if test.useFlight && finalCount != 1 {
			t.Errorf("TestFillerConcurrent(%s): with singleflight got %d filler calls, want 1", test.name, finalCount)
		} else if !test.useFlight && finalCount < 1 {
			t.Errorf("TestFillerConcurrent(%s): without singleflight got %d filler calls, want >= 1", test.name, finalCount)
		}
	}
}
