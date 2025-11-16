// Copyright 2019 Joshua J Baker. All rights reserved.
// Use of this source code is governed by an ISC-style
// license that can be found in the LICENSE file.

package shardmap

import (
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"testing"
	"time"
	"weak"

	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/exp/caches/weak/internal/metrics"
	baseMetrics "github.com/gostdlib/base/telemetry/otel/metrics"
)

var cm = metrics.New(context.MeterProvider(context.Background()).Meter(baseMetrics.MeterName(2) + "/" + "shardmapTest"))

type keyT = string

func k(key int) keyT {
	return strconv.FormatInt(int64(key), 10)
}

func add(x keyT, delta int) int {
	i, err := strconv.ParseInt(x, 10, 64)
	if err != nil {
		panic(err)
	}
	return int(i + int64(delta))
}

// /////////////////////////
func random(N int, perm bool) []keyT {
	nums := make([]keyT, N)
	if perm {
		for i, x := range rand.Perm(N) {
			nums[i] = k(x)
		}
	} else {
		m := make(map[keyT]bool)
		for len(m) < N {
			m[k(int(rand.Uint64()))] = true
		}
		var i int
		for k := range m {
			nums[i] = k
			i++
		}
	}
	return nums
}

func shuffle(nums []keyT) {
	for i := range nums {
		j := rand.Intn(i + 1)
		nums[i], nums[j] = nums[j], nums[i]
	}
}

func TestRandomData(t *testing.T) {
	N := 10000
	start := time.Now()
	for time.Since(start) < time.Second*2 {
		nums := random(N, true)
		m := New[string, string](nil, cm)

		// Keep strong references to prevent GC
		strongRefs := make(map[string]*string)

		v, ok, _ := m.Get(t.Context(), k(999), nil)
		if ok || v != nil {
			t.Fatalf("expected %v, got %v", nil, v)
		}
		v, ok, _ = m.Delete(t.Context(), k(999), nil)
		if ok || v != nil {
			t.Fatalf("expected %v, got %v", nil, v)
		}
		if m.Len() != 0 {
			t.Fatalf("expected %v, got %v", 0, m.Len())
		}
		// set a bunch of items
		for i := 0; i < len(nums); i++ {
			// Create a new heap-allocated string
			val := nums[i]
			ptr := &val
			strongRefs[nums[i]] = ptr
			v, ok, _ := m.Set(t.Context(), nums[i], ptr, nil)
			if ok || v != nil {
				t.Fatalf("expected %v, got %v", nil, v)
			}
		}
		if m.Len() != N {
			t.Fatalf("expected %v, got %v", N, m.Len())
		}
		// retrieve all the items
		shuffle(nums)
		for i := 0; i < len(nums); i++ {
			v, ok, _ := m.Get(t.Context(), nums[i], nil)
			if !ok || *v == "" || *v != nums[i] {
				t.Fatalf("expected %v, got %v", nums[i], *v)
			}
		}
		// replace all the items
		shuffle(nums)
		for i := 0; i < len(nums); i++ {
			// Create a new heap-allocated string
			ptr := new(string)
			*ptr = strconv.Itoa(add(nums[i], 1))
			v, ok, _ := m.Set(t.Context(), nums[i], ptr, nil)
			if !ok || *v != nums[i] {
				t.Fatalf("expected %v, got %v", nums[i], v)
			}
			// Keep the old value alive until we've validated it
			runtime.KeepAlive(v)
			// Now replace the strong reference with the new value
			strongRefs[nums[i]] = ptr
		}
		if m.Len() != N {
			t.Fatalf("expected %v, got %v", N, m.Len())
		}
		// retrieve all the items
		shuffle(nums)
		for i := 0; i < len(nums); i++ {
			v, ok, _ := m.Get(t.Context(), nums[i], nil)
			want := add(nums[i], 1)
			wantStr := strconv.Itoa(want)
			if !ok || *v != wantStr {
				t.Fatalf("expected %v, got %v", add(nums[i], 1), v)
			}
		}
		// remove half the items
		shuffle(nums)
		for i := 0; i < len(nums)/2; i++ {
			v, ok, _ := m.Delete(t.Context(), nums[i], nil)
			want := add(nums[i], 1)
			wantStr := strconv.Itoa(want)
			if !ok || *v != wantStr {
				t.Fatalf("expected %v, got %v", add(nums[i], 1), v)
			}
			// Keep the deleted value alive until we've validated it
			runtime.KeepAlive(v)
		}
		if m.Len() != N/2 {
			t.Fatalf("expected %v, got %v", N/2, m.Len())
		}
		// check to make sure that the items have been removed
		for i := 0; i < len(nums)/2; i++ {
			v, ok, _ := m.Get(t.Context(), nums[i], nil)
			if ok || v != nil {
				t.Fatalf("expected %v, got %v", nil, v)
			}
		}
		// check the second half of the items
		for i := len(nums) / 2; i < len(nums); i++ {
			v, ok, _ := m.Get(t.Context(), nums[i], nil)
			want := add(nums[i], 1)
			wantStr := strconv.Itoa(want)
			if !ok || *v != wantStr {
				t.Fatalf("expected %v, got %v", add(nums[i], 1), v)
			}
		}
		// try to delete again, make sure they don't exist
		for i := 0; i < len(nums)/2; i++ {
			v, ok, _ := m.Delete(t.Context(), nums[i], nil)
			if ok || v != nil {
				t.Fatalf("expected %v, got %v", nil, v)
			}
		}
		if m.Len() != N/2 {
			t.Fatalf("expected %v, got %v", N/2, m.Len())
		}
		for k, v := range m.all() {
			i := add(k, 1)
			str := strconv.Itoa(i)
			if *v != str {
				t.Fatalf("expected %v, got %v", add(k, 1), v)
			}
		}
		var n int
		for range m.all() {
			n++
			break
		}

		if n != 1 {
			t.Fatalf("expected %v, got %v", 1, n)
		}
		for i := len(nums) / 2; i < len(nums); i++ {
			v, ok, _ := m.Delete(t.Context(), nums[i], nil)
			val := add(nums[i], 1)
			valStr := strconv.Itoa(val)
			if !ok || *v != valStr {
				t.Fatalf("expected %v, got %v", add(nums[i], 1), v)
			}
			// Keep the deleted value alive until we've validated it
			runtime.KeepAlive(v)
		}
		// Keep strong references alive until the end of the iteration
		runtime.KeepAlive(strongRefs)
	}
}

func TestClear(t *testing.T) {
	var m Map[string, int]
	// Keep strong references to prevent GC
	strongRefs := make([]*int, 1000)
	for i := 0; i < 1000; i++ {
		// Create a new heap-allocated int
		val := i
		ptr := &val
		strongRefs[i] = ptr
		m.Set(t.Context(), fmt.Sprintf("%d", i), ptr, nil)
	}
	if m.Len() != 1000 {
		t.Fatalf("expected '%v', got '%v'", 1000, m.Len())
	}
	m.Clear()
	if m.Len() != 0 {
		t.Fatalf("expected '%v', got '%v'", 0, m.Len())
	}
	// Keep strong references alive until the end
	runtime.KeepAlive(strongRefs)
}

func TestDeleteIfNil(t *testing.T) {
	tests := []struct {
		name        string
		wantDeleted bool
	}{
		{
			name:        "Success: delete key with nil weak pointer",
			wantDeleted: true,
		},
		{
			name:        "Success: do not delete key with live value",
			wantDeleted: false,
		},
	}

	for _, test := range tests {
		m := New[string, int](nil, cm)

		if test.wantDeleted {
			// Create value that will be GC'd
			func() {
				val := 42
				ptr := &val
				m.Set(t.Context(), "key1", ptr, nil)
			}()

			// Force GC to collect the value
			runtime.GC()
			runtime.GC()
			time.Sleep(10 * time.Millisecond)

			_, deleted := m.DeleteIfNil("key1")
			if !deleted {
				t.Logf("TestDeleteIfNil(%s): WARNING - value not GC'd (non-deterministic)", test.name)
			}
		} else {
			// Keep strong reference
			val := 42
			ptr := &val
			m.Set(t.Context(), "key2", ptr, nil)

			_, deleted := m.DeleteIfNil("key2")
			if deleted {
				t.Errorf("TestDeleteIfNil(%s): got deleted=true, want false", test.name)
			}

			runtime.KeepAlive(ptr)
		}
	}

	// Test non-existent key
	m := New[string, int](nil, cm)
	_, deleted := m.DeleteIfNil("nonexistent")
	if deleted {
		t.Errorf("TestDeleteIfNil: deleted non-existent key")
	}
}

func TestCleanShards(t *testing.T) {
	m := New[string, int](nil, cm)

	// Keep strong references for some values
	strongRefs := make(map[string]*int)

	// Add values
	for i := 0; i < 10; i++ {
		val := i
		ptr := &val
		key := fmt.Sprintf("key%d", i)
		m.Set(t.Context(), key, ptr, nil)

		// Keep strong references only for even numbers
		if i%2 == 0 {
			strongRefs[key] = ptr
		}
	}

	initialLen := m.Len()
	if initialLen != 10 {
		t.Errorf("TestCleanShards: initial Len()=%d, want 10", initialLen)
	}

	// Force GC to collect values without strong references
	runtime.GC()
	runtime.GC()
	time.Sleep(50 * time.Millisecond)

	// Clean shards
	m.CleanShards()

	// Length should be <= initial (some may have been GC'd)
	finalLen := m.Len()
	if finalLen > initialLen {
		t.Errorf("TestCleanShards: after CleanShards, Len()=%d, want <=%d", finalLen, initialLen)
	}

	// Keep strong references alive
	runtime.KeepAlive(strongRefs)
}

func TestLenAtomicCount(t *testing.T) {
	m := New[string, int](nil, cm)

	// Keep strong references
	strongRefs := make(map[string]*int)

	tests := []struct {
		name      string
		op        func()
		wantDelta int
	}{
		{
			name: "Success: Len increases after Set",
			op: func() {
				val := 1
				ptr := &val
				strongRefs["key1"] = ptr
				m.Set(t.Context(), "key1", ptr, nil)
			},
			wantDelta: 1,
		},
		{
			name: "Success: Len unchanged after replace",
			op: func() {
				val := 2
				ptr := &val
				strongRefs["key1"] = ptr
				m.Set(t.Context(), "key1", ptr, nil)
			},
			wantDelta: 0,
		},
		{
			name: "Success: Len increases with new key",
			op: func() {
				val := 3
				ptr := &val
				strongRefs["key2"] = ptr
				m.Set(t.Context(), "key2", ptr, nil)
			},
			wantDelta: 1,
		},
		{
			name: "Success: Len decreases after Delete",
			op: func() {
				m.Delete(t.Context(), "key1", nil)
			},
			wantDelta: -1,
		},
		{
			name: "Success: Len unchanged deleting non-existent",
			op: func() {
				m.Delete(t.Context(), "nonexistent", nil)
			},
			wantDelta: 0,
		},
	}

	currentLen := 0
	for _, test := range tests {
		beforeLen := m.Len()
		test.op()
		afterLen := m.Len()

		currentLen += test.wantDelta

		if afterLen != currentLen {
			t.Errorf("TestLenAtomicCount(%s): Len()=%d, want %d", test.name, afterLen, currentLen)
		}

		if afterLen-beforeLen != test.wantDelta {
			t.Errorf("TestLenAtomicCount(%s): delta=%d, want %d", test.name, afterLen-beforeLen, test.wantDelta)
		}
	}

	// Keep strong references alive
	runtime.KeepAlive(strongRefs)
}

func TestBTreeDeduplication(t *testing.T) {
	// Create a less function that compares weak pointers by their underlying int value
	less := func(a, b weak.Pointer[int]) bool {
		aVal := a.Value()
		bVal := b.Value()
		if aVal == nil && bVal == nil {
			return false
		}
		if aVal == nil {
			return true
		}
		if bVal == nil {
			return false
		}
		return *aVal < *bVal
	}

	m := New[string, int](less, cm)

	// Keep strong references
	strongRefs := make([]*int, 3)

	// Create three different pointers with the same value (heap-allocated)
	strongRefs[0] = new(int)
	strongRefs[1] = new(int)
	strongRefs[2] = new(int)
	*strongRefs[0] = 42
	*strongRefs[1] = 42
	*strongRefs[2] = 42

	// Set three different keys with the same value
	m.Set(t.Context(), "key1", strongRefs[0], nil)
	m.Set(t.Context(), "key2", strongRefs[1], nil)
	m.Set(t.Context(), "key3", strongRefs[2], nil)

	// Get all three values
	v1, ok1, _ := m.Get(t.Context(), "key1", nil)
	v2, ok2, _ := m.Get(t.Context(), "key2", nil)
	v3, ok3, _ := m.Get(t.Context(), "key3", nil)

	switch {
	case !ok1 || !ok2 || !ok3:
		t.Errorf("TestBTreeDeduplication: failed to get all keys")
		return
	case v1 == nil || v2 == nil || v3 == nil:
		t.Errorf("TestBTreeDeduplication: got nil values")
		return
	}

	// All three should point to the same address due to btree deduplication
	if v1 != v2 || v2 != v3 {
		t.Errorf("TestBTreeDeduplication: expected all values to share same pointer, got different pointers")
	}

	// Verify the shared value is correct
	if *v1 != 42 {
		t.Errorf("TestBTreeDeduplication: got value=%d, want 42", *v1)
	}

	runtime.KeepAlive(strongRefs)
}

func TestBTreeDeduplicationWithDeletion(t *testing.T) {
	// Create a less function that compares weak pointers by their underlying int value
	less := func(a, b weak.Pointer[int]) bool {
		aVal := a.Value()
		bVal := b.Value()
		if aVal == nil && bVal == nil {
			return false
		}
		if aVal == nil {
			return true
		}
		if bVal == nil {
			return false
		}
		return *aVal < *bVal
	}

	m := New[string, int](less, cm)

	// Keep strong references
	strongRefs := make([]*int, 2)

	// Create two different pointers with the same value
	val1 := 100
	val2 := 100
	strongRefs[0] = &val1
	strongRefs[1] = &val2

	// Set two keys with the same value
	m.Set(t.Context(), "key1", strongRefs[0], nil)
	m.Set(t.Context(), "key2", strongRefs[1], nil)

	// Get both values to verify they share the same pointer
	v1Before, ok1, _ := m.Get(t.Context(), "key1", nil)
	v2Before, ok2, _ := m.Get(t.Context(), "key2", nil)

	switch {
	case !ok1 || !ok2:
		t.Errorf("TestBTreeDeduplicationWithDeletion: failed to get keys before deletion")
		return
	case v1Before != v2Before:
		t.Errorf("TestBTreeDeduplicationWithDeletion: values don't share pointer before deletion")
		return
	}

	// Delete key1
	deleted, ok, _ := m.Delete(t.Context(), "key1", nil)
	switch {
	case !ok:
		t.Errorf("TestBTreeDeduplicationWithDeletion: failed to delete key1")
		return
	case deleted == nil || *deleted != 100:
		t.Errorf("TestBTreeDeduplicationWithDeletion: deleted wrong value")
		return
	}

	// key2 should still exist with the correct value
	v2After, ok2After, _ := m.Get(t.Context(), "key2", nil)
	switch {
	case !ok2After:
		t.Errorf("TestBTreeDeduplicationWithDeletion: key2 not found after deleting key1")
		return
	case v2After == nil || *v2After != 100:
		t.Errorf("TestBTreeDeduplicationWithDeletion: key2 has wrong value after deletion")
		return
	}

	// key1 should be gone
	v1After, ok1After, _ := m.Get(t.Context(), "key1", nil)
	if ok1After {
		t.Errorf("TestBTreeDeduplicationWithDeletion: key1 still exists after deletion")
	}
	if v1After != nil {
		t.Errorf("TestBTreeDeduplicationWithDeletion: got non-nil value for deleted key")
	}

	runtime.KeepAlive(strongRefs)
}

func TestBTreeNilLessNoDeduplication(t *testing.T) {
	m := New[string, int](nil, cm)

	// Keep strong references
	strongRefs := make([]*int, 2)

	// Create two different pointers with the same value
	val1 := 42
	val2 := 42
	strongRefs[0] = &val1
	strongRefs[1] = &val2

	// Set two keys with the same value
	m.Set(t.Context(), "key1", strongRefs[0], nil)
	m.Set(t.Context(), "key2", strongRefs[1], nil)

	// Get both values
	v1, ok1, _ := m.Get(t.Context(), "key1", nil)
	v2, ok2, _ := m.Get(t.Context(), "key2", nil)

	switch {
	case !ok1 || !ok2:
		t.Errorf("TestBTreeNilLessNoDeduplication: failed to get all keys")
		return
	case v1 == nil || v2 == nil:
		t.Errorf("TestBTreeNilLessNoDeduplication: got nil values")
		return
	}

	// Without btree deduplication, the pointers should be different
	if v1 == v2 {
		t.Errorf("TestBTreeNilLessNoDeduplication: expected different pointers without less function, got same pointer")
	}

	// But both should have the correct value
	switch {
	case *v1 != 42:
		t.Errorf("TestBTreeNilLessNoDeduplication: key1 got value=%d, want 42", *v1)
	case *v2 != 42:
		t.Errorf("TestBTreeNilLessNoDeduplication: key2 got value=%d, want 42", *v2)
	}

	runtime.KeepAlive(strongRefs)
}

func TestBTreeClear(t *testing.T) {
	// Create a less function that compares weak pointers by their underlying int value
	less := func(a, b weak.Pointer[int]) bool {
		aVal := a.Value()
		bVal := b.Value()
		if aVal == nil && bVal == nil {
			return false
		}
		if aVal == nil {
			return true
		}
		if bVal == nil {
			return false
		}
		return *aVal < *bVal
	}

	m := New[string, int](less, cm)

	// Keep strong references
	strongRefs := make([]*int, 3)

	// Add some values
	for i := 0; i < 3; i++ {
		val := i * 10
		strongRefs[i] = &val
		m.Set(t.Context(), fmt.Sprintf("key%d", i), strongRefs[i], nil)
	}

	if m.Len() != 3 {
		t.Errorf("TestBTreeClear: initial Len()=%d, want 3", m.Len())
	}

	// Clear the map
	m.Clear()

	if m.Len() != 0 {
		t.Errorf("TestBTreeClear: after Clear() Len()=%d, want 0", m.Len())
	}

	// Verify keys are gone
	for i := 0; i < 3; i++ {
		v, ok, _ := m.Get(t.Context(), fmt.Sprintf("key%d", i), nil)
		if ok || v != nil {
			t.Errorf("TestBTreeClear: key%d still exists after Clear()", i)
		}
	}

	runtime.KeepAlive(strongRefs)
}
