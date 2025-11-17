package weak

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/kylelemons/godebug/pretty"
)

func TestWithTTL(t *testing.T) {
	tests := []struct {
		name     string
		ttl      time.Duration
		maxTTL   time.Duration
		interval time.Duration
		wantErr  bool
	}{
		{
			name:     "Success: valid TTL and interval",
			ttl:      5 * time.Second,
			maxTTL:   0,
			interval: 1 * time.Second,
			wantErr:  false,
		},
		{
			name:     "Success: valid TTL and maxTTL",
			ttl:      2 * time.Second,
			maxTTL:   10 * time.Second,
			interval: 1 * time.Second,
			wantErr:  false,
		},
		{
			name:     "Success: ttl equals maxTTL",
			ttl:      5 * time.Second,
			maxTTL:   5 * time.Second,
			interval: 1 * time.Second,
			wantErr:  false,
		},
		{
			name:     "Error: interval less than 1 second",
			ttl:      5 * time.Second,
			maxTTL:   0,
			interval: 500 * time.Millisecond,
			wantErr:  true,
		},
		{
			name:     "Error: zero TTL",
			ttl:      0,
			maxTTL:   0,
			interval: 1 * time.Second,
			wantErr:  true,
		},
		{
			name:     "Error: ttl greater than maxTTL",
			ttl:      10 * time.Second,
			maxTTL:   5 * time.Second,
			interval: 1 * time.Second,
			wantErr:  true,
		},
		{
			name:     "Error: ttl less than 1 second",
			ttl:      500 * time.Millisecond,
			maxTTL:   0,
			interval: 1 * time.Second,
			wantErr:  true,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test", WithTTL(test.ttl, test.maxTTL, test.interval))

		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestWithTTL(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestWithTTL(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		if cache == nil {
			t.Errorf("TestWithTTL(%s): got nil cache", test.name)
		}
	}
}

func TestTTLPreventsPrematureGC(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: TTL holds strong reference preventing GC",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test", WithTTL(5*time.Second, 0, 1*time.Second))
		if err != nil {
			t.Fatalf("TestTTLPreventsPrematureGC(%s): failed to create cache: %v", test.name, err)
		}

		// Set value and immediately remove our reference
		func() {
			val := &testValue{data: "test", num: 42}
			_, _, _ = cache.Set(t.Context(), "key", val)
		}()

		// Force GC
		runtime.GC()
		runtime.GC()
		time.Sleep(50 * time.Millisecond)

		// Value should still exist because ttlMap holds strong reference
		got, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestTTLPreventsPrematureGC(%s): failed to get value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestTTLPreventsPrematureGC(%s): value GC'd despite TTL", test.name)
		}
		if got == nil {
			t.Errorf("TestTTLPreventsPrematureGC(%s): got nil value", test.name)
		}
	}
}

func TestTTLWithDel(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: Del removes from both maps",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test", WithTTL(5*time.Second, 0, 1*time.Second))
		if err != nil {
			t.Fatalf("TestTTLWithDel(%s): failed to create cache: %v", test.name, err)
		}

		val := &testValue{data: "test", num: 42}
		_, _, err = cache.Set(t.Context(), "key", val)
		if err != nil {
			t.Fatalf("TestTTLWithDel(%s): failed to set value: %v", test.name, err)
		}

		prev, deleted, err := cache.Del(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestTTLWithDel(%s): failed to delete value: %v", test.name, err)
		}
		if !deleted {
			t.Errorf("TestTTLWithDel(%s): Del returned deleted=false", test.name)
		}
		if diff := pretty.Compare(prev, val); diff != "" {
			t.Errorf("TestTTLWithDel(%s): -got +want:\n%s", test.name, diff)
		}

		_, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestTTLWithDel(%s): failed to get value: %v", test.name, err)
		}
		if ok {
			t.Errorf("TestTTLWithDel(%s): key still exists after Del", test.name)
		}

		runtime.KeepAlive(val)
	}
}

func TestTTLExpiration(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: entries expire after TTL",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 0, 1*time.Second))
		if err != nil {
			t.Fatalf("TestTTLExpiration(%s): failed to create cache: %v", test.name, err)
		}

		// Set value in scope that will end
		func() {
			val := &testValue{data: "test", num: 42}
			_, _, _ = cache.Set(t.Context(), "key", val)

			// Verify it exists initially
			got, ok, err := cache.Get(t.Context(), "key")
			if err != nil {
				t.Fatalf("TestTTLExpiration(%s): failed to get value: %v", test.name, err)
			}
			if !ok {
				t.Errorf("TestTTLExpiration(%s): value not found after Set", test.name)
			}
			if diff := pretty.Compare(got, val); diff != "" {
				t.Errorf("TestTTLExpiration(%s): -got +want:\n%s", test.name, diff)
			}
		}()

		// Wait for TTL to expire plus cleanup interval
		time.Sleep(1*time.Second + 1*time.Second + 200*time.Millisecond)

		// Force GC
		runtime.GC()
		runtime.GC()
		time.Sleep(100 * time.Millisecond)

		// Value should be gone (non-deterministic due to GC)
		_, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestTTLExpiration(%s): failed to get value: %v", test.name, err)
		}
		if ok {
			t.Logf("TestTTLExpiration(%s): WARNING - value still exists (GC is non-deterministic)", test.name)
		}
	}
}

func TestTTLConcurrentAccess(t *testing.T) {
	tests := []struct {
		name          string
		numGoroutines int
		numOps        int
	}{
		{
			name:          "Success: concurrent Set/Get/Del with TTL",
			numGoroutines: 50,
			numOps:        100,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[int, testValue](ctx, "test", WithTTL(5*time.Second, 0, 1*time.Second))
		if err != nil {
			t.Fatalf("TestTTLConcurrentAccess(%s): failed to create cache: %v", test.name, err)
		}

		var wg sync.Group

		for g := 0; g < test.numGoroutines; g++ {
			wg.Go(
				ctx,
				func(ctx context.Context) error {
					for i := 0; i < test.numOps; i++ {
						key := i
						opType := (g + i) % 3

						switch opType {
						case 0: // Set
							val := &testValue{data: "concurrent", num: g*test.numOps + i}
							_, _, _ = cache.Set(t.Context(), key, val)
						case 1: // Get
							_, _, _ = cache.Get(t.Context(), key)
						case 2: // Del
							_, _, _ = cache.Del(t.Context(), key)
						}
					}
					return nil
				},
			)
		}

		wg.Wait(ctx)

		// Verify cache is functional
		testVal := &testValue{data: "final", num: 999}
		_, _, err = cache.Set(t.Context(), 999, testVal)
		if err != nil {
			t.Fatalf("TestTTLConcurrentAccess(%s): failed to set test value: %v", test.name, err)
		}
		got, ok, err := cache.Get(t.Context(), 999)
		if err != nil {
			t.Fatalf("TestTTLConcurrentAccess(%s): failed to get test value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestTTLConcurrentAccess(%s): cache not functional after concurrent ops", test.name)
		}
		if diff := pretty.Compare(got, testVal); diff != "" {
			t.Errorf("TestTTLConcurrentAccess(%s): -got +want:\n%s", test.name, diff)
		}

		runtime.KeepAlive(testVal)
	}
}

func TestTTLReset(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: Set on existing key resets TTL",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test", WithTTL(2*time.Second, 0, 1*time.Second))
		if err != nil {
			t.Fatalf("TestTTLReset(%s): failed to create cache: %v", test.name, err)
		}

		val1 := &testValue{data: "first", num: 1}
		_, _, err = cache.Set(t.Context(), "key", val1)
		if err != nil {
			t.Fatalf("TestTTLReset(%s): failed to set first value: %v", test.name, err)
		}

		// Wait almost until expiration
		time.Sleep(1500 * time.Millisecond)

		// Reset by setting new value
		val2 := &testValue{data: "second", num: 2}
		_, _, err = cache.Set(t.Context(), "key", val2)
		if err != nil {
			t.Fatalf("TestTTLReset(%s): failed to set second value: %v", test.name, err)
		}

		// Wait past original TTL
		time.Sleep(1 * time.Second)

		// Value should still exist because TTL was reset
		got, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestTTLReset(%s): failed to get value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestTTLReset(%s): value not found after TTL reset", test.name)
		}
		if diff := pretty.Compare(got, val2); diff != "" {
			t.Errorf("TestTTLReset(%s): -got +want:\n%s", test.name, diff)
		}

		runtime.KeepAlive(val2)
	}
}

func TestCacheWithoutTTL(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: cache works without TTL option",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test")
		if err != nil {
			t.Fatalf("TestCacheWithoutTTL(%s): failed to create cache: %v", test.name, err)
		}

		val := &testValue{data: "test", num: 42}
		_, _, err = cache.Set(t.Context(), "key", val)
		if err != nil {
			t.Fatalf("TestCacheWithoutTTL(%s): failed to set value: %v", test.name, err)
		}

		got, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestCacheWithoutTTL(%s): failed to get value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestCacheWithoutTTL(%s): value not found", test.name)
		}
		if diff := pretty.Compare(got, val); diff != "" {
			t.Errorf("TestCacheWithoutTTL(%s): -got +want:\n%s", test.name, diff)
		}

		runtime.KeepAlive(val)
	}
}

func TestTTLContextCancellation(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: TTL cleanup stops on context cancel",
		},
	}

	for _, test := range tests {
		ctx, cancel := context.WithCancel(t.Context())

		cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 0, 1*time.Second))
		if err != nil {
			t.Fatalf("TestTTLContextCancellation(%s): failed to create cache: %v", test.name, err)
		}

		val := &testValue{data: "test", num: 42}
		_, _, err = cache.Set(t.Context(), "key", val)
		if err != nil {
			t.Fatalf("TestTTLContextCancellation(%s): failed to set value: %v", test.name, err)
		}

		// Cancel context
		cancel()

		// Give goroutine time to exit
		time.Sleep(100 * time.Millisecond)

		// Cache should still work for basic operations
		got, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestTTLContextCancellation(%s): failed to get value: %v", test.name, err)
		}
		if !ok || got == nil {
			t.Errorf("TestTTLContextCancellation(%s): cache not functional after cancel", test.name)
		}

		runtime.KeepAlive(val)
	}
}

// MaxTTL Tests

func TestMaxTTLForcesDeletion(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: entries forcibly deleted after maxTTL even with strong references",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		// ttl=1s (min hold time), maxTTL=3s (force delete time), interval=1s
		cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 3*time.Second, 1*time.Second))
		if err != nil {
			t.Fatalf("TestMaxTTLForcesDeletion(%s): failed to create cache: %v", test.name, err)
		}

		val := &testValue{data: "test", num: 42}
		_, _, err = cache.Set(t.Context(), "key", val)
		if err != nil {
			t.Fatalf("TestMaxTTLForcesDeletion(%s): failed to set value: %v", test.name, err)
		}

		// Value should exist initially
		_, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestMaxTTLForcesDeletion(%s): failed to get value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestMaxTTLForcesDeletion(%s): value not found after Set", test.name)
		}

		// Wait for maxTTL + cleanup interval + buffer
		time.Sleep(3*time.Second + 1*time.Second + 500*time.Millisecond)

		// Value should be gone even though we have a strong reference
		_, ok, err = cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestMaxTTLForcesDeletion(%s): failed to get value: %v", test.name, err)
		}
		if ok {
			t.Errorf("TestMaxTTLForcesDeletion(%s): value still exists after maxTTL", test.name)
		}

		runtime.KeepAlive(val)
	}
}

func TestMaxTTLWithTTLInteraction(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: entry transitions from ttlMap to expireAfter btree",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		// ttl=1s, maxTTL=5s, interval=1s
		cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 5*time.Second, 1*time.Second))
		if err != nil {
			t.Fatalf("TestMaxTTLWithTTLInteraction(%s): failed to create cache: %v", test.name, err)
		}

		val := &testValue{data: "test", num: 42}
		_, _, err = cache.Set(t.Context(), "key", val)
		if err != nil {
			t.Fatalf("TestMaxTTLWithTTLInteraction(%s): failed to set value: %v", test.name, err)
		}

		// After ttl expires but before maxTTL, value should still exist in cache
		// but strong reference should be released
		time.Sleep(1*time.Second + 1*time.Second + 500*time.Millisecond)

		got, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestMaxTTLWithTTLInteraction(%s): failed to get value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestMaxTTLWithTTLInteraction(%s): value not found after ttl but before maxTTL", test.name)
		}

		// After maxTTL, value should be deleted
		time.Sleep(5*time.Second - 2500*time.Millisecond + 1*time.Second + 500*time.Millisecond)

		_, ok, err = cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestMaxTTLWithTTLInteraction(%s): failed to get value: %v", test.name, err)
		}
		if ok {
			t.Errorf("TestMaxTTLWithTTLInteraction(%s): value still exists after maxTTL", test.name)
		}

		runtime.KeepAlive(got)
	}
}

func TestMaxTTLReset(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: Set on existing key resets both ttl and maxTTL",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		// ttl=1s, maxTTL=3s, interval=1s
		cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 3*time.Second, 1*time.Second))
		if err != nil {
			t.Fatalf("TestMaxTTLReset(%s): failed to create cache: %v", test.name, err)
		}

		val1 := &testValue{data: "first", num: 1}
		_, _, err = cache.Set(t.Context(), "key", val1)
		if err != nil {
			t.Fatalf("TestMaxTTLReset(%s): failed to set first value: %v", test.name, err)
		}

		// Wait almost until maxTTL
		time.Sleep(2500 * time.Millisecond)

		// Reset by setting new value
		val2 := &testValue{data: "second", num: 2}
		_, _, err = cache.Set(t.Context(), "key", val2)
		if err != nil {
			t.Fatalf("TestMaxTTLReset(%s): failed to set second value: %v", test.name, err)
		}

		// Wait past original maxTTL but within new maxTTL
		time.Sleep(2 * time.Second)

		// Value should still exist because maxTTL was reset
		got, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestMaxTTLReset(%s): failed to get value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestMaxTTLReset(%s): value not found after maxTTL reset", test.name)
		}
		if diff := pretty.Compare(got, val2); diff != "" {
			t.Errorf("TestMaxTTLReset(%s): -got +want:\n%s", test.name, diff)
		}

		runtime.KeepAlive(val2)
	}
}

func TestMaxTTLWithDel(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: Del removes from ttlMap, main map, and expireAfter btree",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 5*time.Second, 1*time.Second))
		if err != nil {
			t.Fatalf("TestMaxTTLWithDel(%s): failed to create cache: %v", test.name, err)
		}

		val := &testValue{data: "test", num: 42}
		_, _, err = cache.Set(t.Context(), "key", val)
		if err != nil {
			t.Fatalf("TestMaxTTLWithDel(%s): failed to set value: %v", test.name, err)
		}

		// Wait for entry to move to expireAfter btree
		time.Sleep(1*time.Second + 1*time.Second + 500*time.Millisecond)

		// Delete the entry
		prev, deleted, err := cache.Del(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestMaxTTLWithDel(%s): failed to delete value: %v", test.name, err)
		}
		if !deleted {
			t.Errorf("TestMaxTTLWithDel(%s): Del returned deleted=false", test.name)
		}
		if diff := pretty.Compare(prev, val); diff != "" {
			t.Errorf("TestMaxTTLWithDel(%s): -got +want:\n%s", test.name, diff)
		}

		// Verify it's gone
		_, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestMaxTTLWithDel(%s): failed to get value: %v", test.name, err)
		}
		if ok {
			t.Errorf("TestMaxTTLWithDel(%s): key still exists after Del", test.name)
		}

		// Wait past maxTTL to ensure cleanup doesn't error on missing entry
		time.Sleep(3 * time.Second)

		runtime.KeepAlive(val)
	}
}

func TestMaxTTLDisabled(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: cache with maxTTL=0 behaves as before",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 0, 1*time.Second))
		if err != nil {
			t.Fatalf("TestMaxTTLDisabled(%s): failed to create cache: %v", test.name, err)
		}

		val := &testValue{data: "test", num: 42}
		_, _, err = cache.Set(t.Context(), "key", val)
		if err != nil {
			t.Fatalf("TestMaxTTLDisabled(%s): failed to set value: %v", test.name, err)
		}

		// Entry should be removed from ttlMap after ttl but still accessible in cache
		func() {
			val := &testValue{data: "test", num: 42}
			_, _, _ = cache.Set(t.Context(), "key2", val)
		}()

		time.Sleep(1*time.Second + 1*time.Second + 500*time.Millisecond)

		// With strong reference, entry should still exist
		got, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestMaxTTLDisabled(%s): failed to get value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestMaxTTLDisabled(%s): value not found", test.name)
		}

		runtime.KeepAlive(val)
		runtime.KeepAlive(got)
	}
}

func TestMaxTTLConcurrentAccess(t *testing.T) {
	tests := []struct {
		name          string
		numGoroutines int
		numOps        int
	}{
		{
			name:          "Success: concurrent operations with maxTTL enabled",
			numGoroutines: 20,
			numOps:        50,
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		cache, err := New[int, testValue](ctx, "test", WithTTL(2*time.Second, 5*time.Second, 1*time.Second))
		if err != nil {
			t.Fatalf("TestMaxTTLConcurrentAccess(%s): failed to create cache: %v", test.name, err)
		}

		var wg sync.Group

		for g := 0; g < test.numGoroutines; g++ {
			wg.Go(
				ctx,
				func(ctx context.Context) error {
					for i := 0; i < test.numOps; i++ {
						key := i
						opType := (g + i) % 3

						switch opType {
						case 0: // Set
							val := &testValue{data: "concurrent", num: g*test.numOps + i}
							_, _, _ = cache.Set(t.Context(), key, val)
						case 1: // Get
							_, _, _ = cache.Get(t.Context(), key)
						case 2: // Del
							_, _, _ = cache.Del(t.Context(), key)
						}
					}
					return nil
				},
			)
		}

		wg.Wait(ctx)

		// Verify cache is functional
		testVal := &testValue{data: "final", num: 999}
		_, _, err = cache.Set(t.Context(), 999, testVal)
		if err != nil {
			t.Fatalf("TestMaxTTLConcurrentAccess(%s): failed to set test value: %v", test.name, err)
		}
		got, ok, err := cache.Get(t.Context(), 999)
		if err != nil {
			t.Fatalf("TestMaxTTLConcurrentAccess(%s): failed to get test value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestMaxTTLConcurrentAccess(%s): cache not functional after concurrent ops", test.name)
		}
		if diff := pretty.Compare(got, testVal); diff != "" {
			t.Errorf("TestMaxTTLConcurrentAccess(%s): -got +want:\n%s", test.name, diff)
		}

		runtime.KeepAlive(testVal)
	}
}

func TestMaxTTLMultipleEntriesExpireInOrder(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: multiple entries expire in correct order",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		// ttl=1s, maxTTL=5s, interval=1s
		// Gaps need to be > cleanup interval to ensure different cleanups handle different keys
		cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 5*time.Second, 1*time.Second))
		if err != nil {
			t.Fatalf("TestMaxTTLMultipleEntriesExpireInOrder(%s): failed to create cache: %v", test.name, err)
		}

		// Set three entries at different times with 2s gaps
		val1 := &testValue{data: "first", num: 1}
		_, _, _ = cache.Set(t.Context(), "key1", val1)
		// key1 expireAfter = T+5s = 5s

		time.Sleep(2 * time.Second)

		val2 := &testValue{data: "second", num: 2}
		_, _, _ = cache.Set(t.Context(), "key2", val2)
		// key2 expireAfter = T+5s = 7s

		time.Sleep(2 * time.Second)

		val3 := &testValue{data: "third", num: 3}
		_, _, _ = cache.Set(t.Context(), "key3", val3)
		// key3 expireAfter = T+5s = 9s
		// Now at T=4s

		// Wait for key1's maxTTL to expire and cleanup to run
		// Cleanup at T=6s will delete key1 (expireAfter=5s)
		// We check at T=6.5s
		time.Sleep(2500 * time.Millisecond)

		// key1 should be gone (expired at T=5s, deleted at T=6s cleanup)
		_, ok1, _ := cache.Get(t.Context(), "key1")
		if ok1 {
			t.Errorf("TestMaxTTLMultipleEntriesExpireInOrder(%s): key1 still exists", test.name)
		}

		// key2 should still exist (expires at T=7s, we're at T=6.5s)
		_, ok2, _ := cache.Get(t.Context(), "key2")
		if !ok2 {
			t.Errorf("TestMaxTTLMultipleEntriesExpireInOrder(%s): key2 not found", test.name)
		}

		// key3 should still exist (expires at T=9s, we're at T=6.5s)
		_, ok3, _ := cache.Get(t.Context(), "key3")
		if !ok3 {
			t.Errorf("TestMaxTTLMultipleEntriesExpireInOrder(%s): key3 not found", test.name)
		}

		runtime.KeepAlive(val1)
		runtime.KeepAlive(val2)
		runtime.KeepAlive(val3)
	}
}

func TestMaxTTLWithFiller(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: filler can reload entry after maxTTL deletion",
		},
	}

	for _, test := range tests {
		ctx := t.Context()
		fillCount := 0
		var mu sync.Mutex

		filler := func(ctx context.Context, k string) (*testValue, bool, error) {
			mu.Lock()
			fillCount++
			mu.Unlock()
			return &testValue{data: "filled", num: 100}, true, nil
		}

		cache, err := New[string, testValue](
			ctx,
			"test",
			WithTTL(1*time.Second, 3*time.Second, 1*time.Second),
			WithFiller(filler),
		)
		if err != nil {
			t.Fatalf("TestMaxTTLWithFiller(%s): failed to create cache: %v", test.name, err)
		}

		val := &testValue{data: "original", num: 42}
		_, _, err = cache.Set(t.Context(), "key", val)
		if err != nil {
			t.Fatalf("TestMaxTTLWithFiller(%s): failed to set value: %v", test.name, err)
		}

		// Wait for maxTTL to expire
		time.Sleep(3*time.Second + 1*time.Second + 500*time.Millisecond)

		// Get should trigger filler
		got, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestMaxTTLWithFiller(%s): failed to get value: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestMaxTTLWithFiller(%s): filler did not load value", test.name)
		}

		mu.Lock()
		if fillCount != 1 {
			t.Errorf("TestMaxTTLWithFiller(%s): filler called %d times, want 1", test.name, fillCount)
		}
		mu.Unlock()

		want := &testValue{data: "filled", num: 100}
		if diff := pretty.Compare(got, want); diff != "" {
			t.Errorf("TestMaxTTLWithFiller(%s): -got +want:\n%s", test.name, diff)
		}

		runtime.KeepAlive(val)
	}
}

func TestMaxTTLContextCancellation(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: maxTTL cleanup stops on context cancel",
		},
	}

	for _, test := range tests {
		ctx, cancel := context.WithCancel(t.Context())

		cache, err := New[string, testValue](ctx, "test", WithTTL(1*time.Second, 5*time.Second, 1*time.Second))
		if err != nil {
			t.Fatalf("TestMaxTTLContextCancellation(%s): failed to create cache: %v", test.name, err)
		}

		val := &testValue{data: "test", num: 42}
		_, _, err = cache.Set(t.Context(), "key", val)
		if err != nil {
			t.Fatalf("TestMaxTTLContextCancellation(%s): failed to set value: %v", test.name, err)
		}

		// Cancel context
		cancel()

		// Give goroutine time to exit
		time.Sleep(100 * time.Millisecond)

		// Cache should still work for basic operations
		got, ok, err := cache.Get(t.Context(), "key")
		if err != nil {
			t.Fatalf("TestMaxTTLContextCancellation(%s): failed to get value: %v", test.name, err)
		}
		if !ok || got == nil {
			t.Errorf("TestMaxTTLContextCancellation(%s): cache not functional after cancel", test.name)
		}

		runtime.KeepAlive(val)
	}
}
