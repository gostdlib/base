//go:build memory_test

package weak

import (
	"runtime"
	"testing"
	"time"

	"github.com/gostdlib/base/context"
)

// largeValue is a struct designed to consume approximately 100 bytes of memory per entry.
type largeValue struct {
	data [100]byte
}

// TestMemoryReclamationAfterTTL tests that memory used by cache entries is properly reclaimed after their TTL expires
// and garbage collection is forced. This is a crucial test to ensure that the cache does not lead to memory leaks
// over time as entries expire.
func TestMemoryReclamationAfterTTL(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Success: memory is reclaimed after TTL expiration and GC",
		},
	}

	for _, test := range tests {
		ctx := context.Background()

		// Force GC and get initial memory baseline
		runtime.GC()
		runtime.GC()
		time.Sleep(100 * time.Millisecond)

		var initialMem runtime.MemStats
		runtime.ReadMemStats(&initialMem)
		initialAlloc := initialMem.Alloc

		// Create cache with 1 minute TTL
		cache, err := New[int, largeValue](ctx, "memory-test", WithTTL(30*time.Second, 10*time.Second))
		if err != nil {
			t.Fatalf("TestMemoryReclamationAfterTTL(%s): failed to create cache: %v", test.name, err)
		}

		// Add 1 million entries (~100 MiB)
		t.Logf("TestMemoryReclamationAfterTTL(%s): Adding 1 million entries...", test.name)
		for i := 0; i < 1_000_000; i++ {
			val := &largeValue{}
			// Fill with some data to ensure memory is actually allocated
			for j := range val.data {
				val.data[j] = byte(i % 256)
			}
			_, _, err := cache.Set(context.Background(), i, val)
			if err != nil {
				t.Fatalf("TestMemoryReclamationAfterTTL(%s): failed to set value %d: %v", test.name, i, err)
			}
		}

		// Measure memory after adding entries
		runtime.GC()
		time.Sleep(100 * time.Millisecond)

		var afterSetMem runtime.MemStats
		runtime.ReadMemStats(&afterSetMem)
		afterSetAlloc := afterSetMem.Alloc
		memoryUsed := afterSetAlloc - initialAlloc
		memoryUsedMiB := float64(memoryUsed) / (1024 * 1024)

		t.Logf("TestMemoryReclamationAfterTTL(%s): Initial memory: %.2f MiB", test.name, float64(initialAlloc)/(1024*1024))
		t.Logf("TestMemoryReclamationAfterTTL(%s): After adding entries: %.2f MiB", test.name, float64(afterSetAlloc)/(1024*1024))
		t.Logf("TestMemoryReclamationAfterTTL(%s): Memory used by cache: %.2f MiB", test.name, memoryUsedMiB)

		// Verify we're using roughly the expected amount of memory (at least 80 MiB)
		// Allow for overhead from the cache structures
		if memoryUsedMiB < 80 {
			t.Errorf("TestMemoryReclamationAfterTTL(%s): expected at least 80 MiB used, got %.2f MiB", test.name, memoryUsedMiB)
		}

		// Sleep for 1 minute to allow TTL to expire
		t.Logf("TestMemoryReclamationAfterTTL(%s): Sleeping for 1 minute to allow TTL expiration...", test.name)
		time.Sleep(1 * time.Minute)

		// Force garbage collection
		t.Logf("TestMemoryReclamationAfterTTL(%s): Forcing first garbage collection...", test.name)
		runtime.GC()

		// Sleep to allow finalizers to run
		t.Logf("TestMemoryReclamationAfterTTL(%s): Sleeping 1 second for finalizers...", test.name)
		time.Sleep(1 * time.Second)

		// Force second garbage collection
		t.Logf("TestMemoryReclamationAfterTTL(%s): Forcing second garbage collection...", test.name)
		runtime.GC()

		// Measure memory after GC
		var afterGCMem runtime.MemStats
		runtime.ReadMemStats(&afterGCMem)
		afterGCAlloc := afterGCMem.Alloc
		memoryAfterGC := afterGCAlloc - initialAlloc
		memoryAfterGCMiB := float64(memoryAfterGC) / (1024 * 1024)

		t.Logf("TestMemoryReclamationAfterTTL(%s): After GC: %.2f MiB", test.name, float64(afterGCAlloc)/(1024*1024))
		t.Logf("TestMemoryReclamationAfterTTL(%s): Memory used after GC: %.2f MiB", test.name, memoryAfterGCMiB)

		// Calculate reduction
		memoryReduction := memoryUsed - memoryAfterGC
		memoryReductionMiB := float64(memoryReduction) / (1024 * 1024)
		reductionPercent := (float64(memoryReduction) / float64(memoryUsed)) * 100

		t.Logf("TestMemoryReclamationAfterTTL(%s): Memory reclaimed: %.2f MiB (%.1f%%)", test.name, memoryReductionMiB, reductionPercent)

		// Verify drastic reduction - at least 70% of the memory should be reclaimed
		// This accounts for some overhead from the cache structure itself
		if reductionPercent < 70 {
			t.Errorf("TestMemoryReclamationAfterTTL(%s): expected at least 70%% memory reduction, got %.1f%%", test.name, reductionPercent)
		}

		// Verify we're close to initial memory (within 35 MiB)
		if memoryAfterGCMiB > 35 {
			t.Errorf("TestMemoryReclamationAfterTTL(%s): expected memory usage close to initial (< 35 MiB overhead), got %.2f MiB", test.name, memoryAfterGCMiB)
		}

		// Verify cache is still functional
		testVal := &largeValue{}
		for i := range testVal.data {
			testVal.data[i] = 42
		}
		_, _, err = cache.Set(context.Background(), 9999999, testVal)
		if err != nil {
			t.Fatalf("TestMemoryReclamationAfterTTL(%s): cache not functional after GC: %v", test.name, err)
		}

		got, ok, err := cache.Get(context.Background(), 9999999)
		if err != nil {
			t.Fatalf("TestMemoryReclamationAfterTTL(%s): Get failed after GC: %v", test.name, err)
		}
		if !ok {
			t.Errorf("TestMemoryReclamationAfterTTL(%s): cache not functional after GC", test.name)
		}
		if got == nil {
			t.Errorf("TestMemoryReclamationAfterTTL(%s): got nil value from cache", test.name)
		}

		runtime.KeepAlive(testVal)
		runtime.KeepAlive(cache)
	}
}
