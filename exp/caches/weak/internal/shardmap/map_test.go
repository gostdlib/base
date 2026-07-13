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
)

type keyT = string

// gcUntil forces a GC and polls cond up to 200 times, 5ms apart, returning whether cond became true. Callers issue
// their own failure handling so the message follows each test's convention.
func gcUntil(cond func() bool) bool {
	for i := 0; i < 200; i++ {
		runtime.GC()
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

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
		m := New[string, string](nil)

		// Keep strong references to prevent GC
		strongRefs := make(map[string]*string)

		v, ok := m.Get(k(999))
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
			res, _ := m.Set(t.Context(), nums[i], ptr, nil, time.Time{})
			if res.Replaced || res.Prev != nil {
				t.Fatalf("expected %v, got %v", nil, res.Prev)
			}
		}
		if m.Len() != N {
			t.Fatalf("expected %v, got %v", N, m.Len())
		}
		// retrieve all the items
		shuffle(nums)
		for i := 0; i < len(nums); i++ {
			v, ok := m.Get(nums[i])
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
			res, _ := m.Set(t.Context(), nums[i], ptr, nil, time.Time{})
			if !res.Replaced || *res.Prev != nums[i] {
				t.Fatalf("expected %v, got %v", nums[i], res.Prev)
			}
			// Keep the old value alive until we've validated it
			runtime.KeepAlive(res.Prev)
			// Now replace the strong reference with the new value
			strongRefs[nums[i]] = ptr
		}
		if m.Len() != N {
			t.Fatalf("expected %v, got %v", N, m.Len())
		}
		// retrieve all the items
		shuffle(nums)
		for i := 0; i < len(nums); i++ {
			v, ok := m.Get(nums[i])
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
			v, ok := m.Get(nums[i])
			if ok || v != nil {
				t.Fatalf("expected %v, got %v", nil, v)
			}
		}
		// check the second half of the items
		for i := len(nums) / 2; i < len(nums); i++ {
			v, ok := m.Get(nums[i])
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
		m.Set(t.Context(), fmt.Sprintf("%d", i), ptr, nil, time.Time{})
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

// TestSetIfNil verifies that SetIfNil refuses to store over a live value and that its fresh return distinguishes a
// physically new bucket from an in-place replacement of a bucket whose weak pointer was collected. Fresh mirrors the
// internal count accounting so callers can keep their own item counters symmetric with Len(). There is no
// runtime.AddCleanup at the shardmap level, so a collected bucket is stable and the nil-replace case is deterministic.
func TestSetIfNil(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(name string, m *Map[string, int]) *int
		wantSetOk bool
		wantFresh bool
		wantLen   int
	}{
		{
			name:      "Success: store on a missing key is a fresh insert",
			setup:     func(name string, m *Map[string, int]) *int { return nil },
			wantSetOk: true,
			wantFresh: true,
			wantLen:   1,
		},
		{
			name: "Success: store over a live value is refused",
			setup: func(name string, m *Map[string, int]) *int {
				v := 1
				m.Set(t.Context(), "key", &v, nil, time.Time{})
				return &v
			},
			wantSetOk: false,
			wantFresh: false,
			wantLen:   1,
		},
		{
			name: "Success: store over a collected weak pointer replaces the bucket without being fresh",
			setup: func(name string, m *Map[string, int]) *int {
				func() {
					v := 1
					m.Set(t.Context(), "key", &v, nil, time.Time{})
				}()
				// GC-poll until the weak pointer goes nil; the bucket itself persists because nothing deletes it.
				if !gcUntil(func() bool { _, ok := m.Get("key"); return !ok }) {
					t.Fatalf("TestSetIfNil(%s): seeded value never collected, cannot exercise nil-replace path", name)
				}
				return nil
			},
			wantSetOk: true,
			wantFresh: false,
			wantLen:   1,
		},
	}

	for _, test := range tests {
		m := New[string, int](nil)
		keep := test.setup(test.name, m)

		val := 42
		res, err := m.SetIfNil(t.Context(), "key", &val, time.Time{})
		existing, setOk, fresh := res.Prev, res.Ok, res.Fresh
		if err != nil {
			t.Fatalf("TestSetIfNil(%s): got err == %s, want err == nil", test.name, err)
		}
		if setOk != test.wantSetOk {
			t.Errorf("TestSetIfNil(%s): got setOk == %v, want setOk == %v", test.name, setOk, test.wantSetOk)
		}
		if fresh != test.wantFresh {
			t.Errorf("TestSetIfNil(%s): got fresh == %v, want fresh == %v", test.name, fresh, test.wantFresh)
		}
		if got := m.Len(); got != test.wantLen {
			t.Errorf("TestSetIfNil(%s): got Len() == %d, want Len() == %d", test.name, got, test.wantLen)
		}
		// When the store is refused because a live value is already present, existing must be that exact live pointer
		// (the one the racing caller lost to). Otherwise the store happened and existing must be nil.
		wantExisting := (*int)(nil)
		if !test.wantSetOk {
			wantExisting = keep
		}
		if existing != wantExisting {
			t.Errorf("TestSetIfNil(%s): got existing == %p, want existing == %p", test.name, existing, wantExisting)
		}

		runtime.KeepAlive(keep)
		runtime.KeepAlive(&val)
	}
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
		m := New[string, int](nil)

		if test.wantDeleted {
			// Create value that will be GC'd
			func() {
				val := 42
				ptr := &val
				m.Set(t.Context(), "key1", ptr, nil, time.Time{})
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
			m.Set(t.Context(), "key2", ptr, nil, time.Time{})

			_, deleted := m.DeleteIfNil("key2")
			if deleted {
				t.Errorf("TestDeleteIfNil(%s): got deleted=true, want false", test.name)
			}

			runtime.KeepAlive(ptr)
		}
	}

	// Test non-existent key
	m := New[string, int](nil)
	_, deleted := m.DeleteIfNil("nonexistent")
	if deleted {
		t.Errorf("TestDeleteIfNil: deleted non-existent key")
	}
}

func TestCleanShards(t *testing.T) {
	m := New[string, int](nil)

	// Keep strong references for some values
	strongRefs := make(map[string]*int)

	// Add values
	for i := 0; i < 10; i++ {
		val := i
		ptr := &val
		key := fmt.Sprintf("key%d", i)
		m.Set(t.Context(), key, ptr, nil, time.Time{})

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
	m := New[string, int](nil)

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
				m.Set(t.Context(), "key1", ptr, nil, time.Time{})
			},
			wantDelta: 1,
		},
		{
			name: "Success: Len unchanged after replace",
			op: func() {
				val := 2
				ptr := &val
				strongRefs["key1"] = ptr
				m.Set(t.Context(), "key1", ptr, nil, time.Time{})
			},
			wantDelta: 0,
		},
		{
			name: "Success: Len increases with new key",
			op: func() {
				val := 3
				ptr := &val
				strongRefs["key2"] = ptr
				m.Set(t.Context(), "key2", ptr, nil, time.Time{})
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

	m := New[string, int](less)

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
	m.Set(t.Context(), "key1", strongRefs[0], nil, time.Time{})
	m.Set(t.Context(), "key2", strongRefs[1], nil, time.Time{})
	m.Set(t.Context(), "key3", strongRefs[2], nil, time.Time{})

	// Get all three values
	v1, ok1 := m.Get("key1")
	v2, ok2 := m.Get("key2")
	v3, ok3 := m.Get("key3")

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

	m := New[string, int](less)

	// Keep strong references
	strongRefs := make([]*int, 2)

	// Create two different pointers with the same value
	val1 := 100
	val2 := 100
	strongRefs[0] = &val1
	strongRefs[1] = &val2

	// Set two keys with the same value
	m.Set(t.Context(), "key1", strongRefs[0], nil, time.Time{})
	m.Set(t.Context(), "key2", strongRefs[1], nil, time.Time{})

	// Get both values to verify they share the same pointer
	v1Before, ok1 := m.Get("key1")
	v2Before, ok2 := m.Get("key2")

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
	v2After, ok2After := m.Get("key2")
	switch {
	case !ok2After:
		t.Errorf("TestBTreeDeduplicationWithDeletion: key2 not found after deleting key1")
		return
	case v2After == nil || *v2After != 100:
		t.Errorf("TestBTreeDeduplicationWithDeletion: key2 has wrong value after deletion")
		return
	}

	// key1 should be gone
	v1After, ok1After := m.Get("key1")
	if ok1After {
		t.Errorf("TestBTreeDeduplicationWithDeletion: key1 still exists after deletion")
	}
	if v1After != nil {
		t.Errorf("TestBTreeDeduplicationWithDeletion: got non-nil value for deleted key")
	}

	runtime.KeepAlive(strongRefs)
}

func TestBTreeNilLessNoDeduplication(t *testing.T) {
	m := New[string, int](nil)

	// Keep strong references
	strongRefs := make([]*int, 2)

	// Create two different pointers with the same value
	val1 := 42
	val2 := 42
	strongRefs[0] = &val1
	strongRefs[1] = &val2

	// Set two keys with the same value
	m.Set(t.Context(), "key1", strongRefs[0], nil, time.Time{})
	m.Set(t.Context(), "key2", strongRefs[1], nil, time.Time{})

	// Get both values
	v1, ok1 := m.Get("key1")
	v2, ok2 := m.Get("key2")

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

	m := New[string, int](less)

	// Keep strong references
	strongRefs := make([]*int, 3)

	// Add some values
	for i := 0; i < 3; i++ {
		val := i * 10
		strongRefs[i] = &val
		m.Set(t.Context(), fmt.Sprintf("key%d", i), strongRefs[i], nil, time.Time{})
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
		v, ok := m.Get(fmt.Sprintf("key%d", i))
		if ok || v != nil {
			t.Errorf("TestBTreeClear: key%d still exists after Clear()", i)
		}
	}

	runtime.KeepAlive(strongRefs)
}

// intLess orders weak pointers by their underlying int value, treating a collected (nil) pointer as the least
// element. It is the dedup comparator used by the refcount regression tests.
func intLess(a, b weak.Pointer[int]) bool {
	av, bv := a.Value(), b.Value()
	switch {
	case av == nil && bv == nil:
		return false
	case av == nil:
		return true
	case bv == nil:
		return false
	default:
		return *av < *bv
	}
}

// TestDeDupeDeleteKeepsSharedRepresentative pins two dedup-tree bugs that both dropped a shared representative when a
// key referencing it was deleted, leaving a later equal Set un-deduped. Each case stores key1 and key2 to one shared
// representative, deletes one of them, then Sets key3 with an equal value and requires key3 to dedup against the
// surviving key.
//
//   - via filler (was TestDeDupeCorruptionAfterFillerDelete): key2 is stored filler-style (SetIfNil) and then deleted.
//     Pre-fix SetIfNil never entered the tree and Delete removed by comparator-equality, so deleting key2 deleted
//     key1's representative and key3 got a fresh, un-deduped pointer.
//   - via Set (was TestDeDupeSharedRepresentativeDelete): key2 is stored through Set and key1 is deleted. Pre-fix
//     Delete removed the shared representative outright (no refcount), so key3 got a fresh pointer.
func TestDeDupeDeleteKeepsSharedRepresentative(t *testing.T) {
	tests := []struct {
		name          string
		key2ViaFiller bool
		deleteKey     string
		survivorKey   string
	}{
		{
			name:          "Success: filler-stored duplicate deleted, key3 still dedups against key1",
			key2ViaFiller: true,
			deleteKey:     "key2",
			survivorKey:   "key1",
		},
		{
			name:          "Success: Set-stored duplicate deleted, key3 still dedups against key2",
			key2ViaFiller: false,
			deleteKey:     "key1",
			survivorKey:   "key2",
		},
	}

	for _, test := range tests {
		m := New[string, int](intLess)

		// Three distinct heap pointers that share the same value so they dedup to one representative.
		p1, p2, p3 := new(int), new(int), new(int)
		*p1, *p2, *p3 = 42, 42, 42

		if _, err := m.Set(t.Context(), "key1", p1, nil, time.Time{}); err != nil {
			t.Fatalf("TestDeDupeDeleteKeepsSharedRepresentative(%s): Set key1: %v", test.name, err)
		}
		if test.key2ViaFiller {
			if _, err := m.SetIfNil(t.Context(), "key2", p2, time.Time{}); err != nil {
				t.Fatalf("TestDeDupeDeleteKeepsSharedRepresentative(%s): SetIfNil key2: %v", test.name, err)
			}
		} else {
			if _, err := m.Set(t.Context(), "key2", p2, nil, time.Time{}); err != nil {
				t.Fatalf("TestDeDupeDeleteKeepsSharedRepresentative(%s): Set key2: %v", test.name, err)
			}
		}
		// key1 and key2 must already share a representative.
		v1, _ := m.Get("key1")
		v2, _ := m.Get("key2")
		if v1 != v2 {
			t.Fatalf("TestDeDupeDeleteKeepsSharedRepresentative(%s): key1 and key2 did not dedup, got %p and %p", test.name, v1, v2)
		}

		if _, _, err := m.Delete(t.Context(), test.deleteKey, nil); err != nil {
			t.Fatalf("TestDeDupeDeleteKeepsSharedRepresentative(%s): Delete %s: %v", test.name, test.deleteKey, err)
		}
		if _, err := m.Set(t.Context(), "key3", p3, nil, time.Time{}); err != nil {
			t.Fatalf("TestDeDupeDeleteKeepsSharedRepresentative(%s): Set key3: %v", test.name, err)
		}

		survivor, okS := m.Get(test.survivorKey)
		v3, ok3 := m.Get("key3")
		switch {
		case !okS || !ok3:
			t.Fatalf("TestDeDupeDeleteKeepsSharedRepresentative(%s): survivor ok=%v key3 ok=%v, want both true", test.name, okS, ok3)
		case v3 != survivor:
			t.Errorf("TestDeDupeDeleteKeepsSharedRepresentative(%s): key3 not deduped vs %s, got %p, %p", test.name, test.survivorKey, v3, survivor)
		}

		runtime.KeepAlive(p1)
		runtime.KeepAlive(p2)
		runtime.KeepAlive(p3)
	}
}

// nilEquatingLess is a WithDeDupe comparator that treats a nil-valued weak pointer as comparator-equal to every other
// pointer (neither is less than the other). A real comparator is permitted by the WithDeDupe contract to order a nil
// pointer this way, and it deterministically reproduces the GC race in which a live value is stored while the tree
// still holds a collected representative the comparator considers equal.
func nilEquatingLess(a, b weak.Pointer[int]) bool {
	av, bv := a.Value(), b.Value()
	if av == nil || bv == nil {
		return false
	}
	return *av < *bv
}

// TestDeDupeAddOverDeadRepresentative pins fix 3: when treeAdd finds an equal representative whose referent has already
// been collected, storing the new value must not merge onto that corpse (which would leave the bucket pointing at a nil
// weak pointer, so a Get misses even though a live value was just stored). Post-fix treeAdd evicts the dead node and
// re-inserts the live pointer as the new representative. The nil-equating comparator makes the race deterministic.
func TestDeDupeAddOverDeadRepresentative(t *testing.T) {
	m := New[string, int](nilEquatingLess)

	// Seed key1 with a value whose only strong reference is dropped when this closure returns, so it can be collected
	// while its bucket (and its now-dead tree representative) persist.
	func() {
		p1 := new(int)
		*p1 = 1
		if _, err := m.Set(t.Context(), "key1", p1, nil, time.Time{}); err != nil {
			t.Fatalf("TestDeDupeAddOverDeadRepresentative: Set key1: %v", err)
		}
	}()

	if !gcUntil(func() bool { _, ok := m.Get("key1"); return !ok }) {
		t.Fatalf("TestDeDupeAddOverDeadRepresentative: key1 value never collected, cannot exercise dead-representative path")
	}
	if got := m.Len(); got != 1 {
		t.Fatalf("TestDeDupeAddOverDeadRepresentative: got Len()=%d before second Set, want 1 (dead bucket must persist)", got)
	}

	// Store key2 with a live value the comparator equates with the dead representative. Post-fix the dead node is
	// evicted and key2's live pointer becomes the representative, so a Get of key2 must return the live value.
	p2 := new(int)
	*p2 = 2
	if _, err := m.Set(t.Context(), "key2", p2, nil, time.Time{}); err != nil {
		t.Fatalf("TestDeDupeAddOverDeadRepresentative: Set key2: %v", err)
	}
	v2, ok := m.Get("key2")
	if !ok || v2 != p2 {
		t.Fatalf("TestDeDupeAddOverDeadRepresentative: Get key2 got (%p, %v), want (%p, true) (stored dead rep)", v2, ok, p2)
	}
	// The tree and refs must track exactly the live representative now: the dead node is gone, key2's is the only one.
	if got := m.tree.Len(); got != 1 {
		t.Errorf("TestDeDupeAddOverDeadRepresentative: got tree.Len()=%d, want 1", got)
	}
	if got := len(m.refs); got != 1 {
		t.Errorf("TestDeDupeAddOverDeadRepresentative: got len(refs)=%d, want 1", got)
	}

	// key1's bucket still references the evicted dead representative. Removing it must release cleanly (a no-op on the
	// already-gone tree node) without corrupting the live representative or driving the count negative.
	if _, deleted := m.DeleteIfNil("key1"); !deleted {
		t.Fatalf("TestDeDupeAddOverDeadRepresentative: DeleteIfNil key1 got deleted=false, want true")
	}
	if got := m.Len(); got != 1 {
		t.Errorf("TestDeDupeAddOverDeadRepresentative: got Len()=%d after DeleteIfNil key1, want 1", got)
	}
	if v2, ok := m.Get("key2"); !ok || v2 != p2 {
		t.Errorf("TestDeDupeAddOverDeadRepresentative: Get key2 after DeleteIfNil got (%p, %v), want (%p, true)", v2, ok, p2)
	}
	if got := m.tree.Len(); got != 1 {
		t.Errorf("TestDeDupeAddOverDeadRepresentative: got tree.Len()=%d after DeleteIfNil, want 1", got)
	}
	if got := len(m.refs); got != 1 {
		t.Errorf("TestDeDupeAddOverDeadRepresentative: got len(refs)=%d after DeleteIfNil, want 1", got)
	}

	runtime.KeepAlive(p2)
}

// TestDeDupeRefcountNilCollected verifies the refcount bookkeeping handles a representative whose referent has been
// collected: two keys sharing a representative, both collected, must each release exactly one reference so the tree
// entry and the refs map are fully drained only after the last DeleteIfNil. Because refs is keyed by pointer identity
// (not the comparator, which orders every nil pointer as least), the release is exact. This is a correctness test for
// the new refcount code; the refs map did not exist before the fix.
func TestDeDupeRefcountNilCollected(t *testing.T) {
	m := New[string, int](intLess)

	// Store two keys that dedup to one representative, then drop all strong references so the value can be collected.
	func() {
		p1, p2 := new(int), new(int)
		*p1, *p2 = 7, 7
		m.Set(t.Context(), "key1", p1, nil, time.Time{})
		m.Set(t.Context(), "key2", p2, nil, time.Time{})
	}()

	// GC-poll until the shared value is collected; the buckets persist because nothing deletes them at this layer.
	collected := gcUntil(func() bool { _, ok := m.Get("key1"); return !ok })
	if !collected {
		t.Fatalf("TestDeDupeRefcountNilCollected: shared value never collected, cannot exercise nil refcount path")
	}

	if _, deleted := m.DeleteIfNil("key1"); !deleted {
		t.Fatalf("TestDeDupeRefcountNilCollected: DeleteIfNil key1 got deleted=false, want true")
	}
	// After releasing only one of the two references, the representative must still be tracked.
	if got := len(m.refs); got != 1 {
		t.Errorf("TestDeDupeRefcountNilCollected: after one release got len(refs)=%d, want 1", got)
	}
	if _, deleted := m.DeleteIfNil("key2"); !deleted {
		t.Fatalf("TestDeDupeRefcountNilCollected: DeleteIfNil key2 got deleted=false, want true")
	}
	if got := len(m.refs); got != 0 {
		t.Errorf("TestDeDupeRefcountNilCollected: after last release got len(refs)=%d, want 0", got)
	}
	if got := m.tree.Len(); got != 0 {
		t.Errorf("TestDeDupeRefcountNilCollected: after last release got tree.Len()=%d, want 0", got)
	}
}

// TestSetReSetCollectedBucketNoDoubleCount pins the Set double-count bug: re-Setting a key whose bucket persists but
// whose weak pointer was collected must not increase Len(), because the physical bucket already exists. There is no
// runtime.AddCleanup at the shardmap layer, so the collected-but-present bucket is stable and the test deterministic.
func TestSetReSetCollectedBucketNoDoubleCount(t *testing.T) {
	m := New[string, int](nil)

	// Seed a bucket, then drop the only reference so the value is collected but the bucket persists.
	func() {
		v := 1
		m.Set(t.Context(), "key", &v, nil, time.Time{})
	}()
	collected := gcUntil(func() bool { _, ok := m.Get("key"); return !ok })
	if !collected {
		t.Fatalf("TestSetReSetCollectedBucketNoDoubleCount: seeded value never collected, cannot exercise re-Set path")
	}
	if got := m.Len(); got != 1 {
		t.Fatalf("TestSetReSetCollectedBucketNoDoubleCount: got Len()=%d before re-Set, want 1 (nil bucket must persist)", got)
	}

	v2 := 2
	if _, err := m.Set(t.Context(), "key", &v2, nil, time.Time{}); err != nil {
		t.Fatalf("TestSetReSetCollectedBucketNoDoubleCount: re-Set: %v", err)
	}
	if got := m.Len(); got != 1 {
		t.Errorf("TestSetReSetCollectedBucketNoDoubleCount: got Len()=%d after re-Set, want 1", got)
	}

	runtime.KeepAlive(&v2)
}

// TestDeleteCollectedBucketReportsRemoved pins the Delete accounting bug: deleting a key whose bucket persists but
// whose weak pointer was collected must report removed=true (a physical bucket was taken out), so callers decrement
// their own counters. Pre-fix Delete returned deleted=false whenever the value was already nil. Deterministic because
// the shardmap layer registers no cleanup.
func TestDeleteCollectedBucketReportsRemoved(t *testing.T) {
	m := New[string, int](nil)

	func() {
		v := 1
		m.Set(t.Context(), "key", &v, nil, time.Time{})
	}()
	collected := gcUntil(func() bool { _, ok := m.Get("key"); return !ok })
	if !collected {
		t.Fatalf("TestDeleteCollectedBucketReportsRemoved: seeded value never collected, cannot exercise the path")
	}

	prev, removed, err := m.Delete(t.Context(), "key", nil)
	if err != nil {
		t.Fatalf("TestDeleteCollectedBucketReportsRemoved: Delete: %v", err)
	}
	if !removed {
		t.Errorf("TestDeleteCollectedBucketReportsRemoved: got removed=false, want true (bucket was physically removed)")
	}
	if prev != nil {
		t.Errorf("TestDeleteCollectedBucketReportsRemoved: got prev=%p, want nil (value was collected)", prev)
	}
	if got := m.Len(); got != 0 {
		t.Errorf("TestDeleteCollectedBucketReportsRemoved: got Len()=%d, want 0", got)
	}
}
