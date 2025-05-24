// Copyright 2019 Joshua J Baker. All rights reserved.
// Use of this source code is governed by an ISC-style
// license that can be found in the LICENSE file.

package shardmap

import (
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"time"
)

type keyT = string
type valueT = interface{}

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

func init() {
	//var seed int64 = 1519776033517775607
	seed := (time.Now().UnixNano())
	println("seed:", seed)
	rand.Seed(seed)
}

func TestRandomData(t *testing.T) {
	N := 10000
	start := time.Now()
	for time.Since(start) < time.Second*2 {
		nums := random(N, true)
		var m *Map[string, any]
		switch rand.Int() % 5 {
		default:
			m = New[string, any](N / ((rand.Int() % 3) + 1))
		case 1:
			m = new(Map[string, any])
		case 2:
			m = New[string, any](0)
		}
		v, ok := m.Get(k(999))
		if ok || v != nil {
			t.Fatalf("expected %v, got %v", nil, v)
		}
		v, ok = m.Delete(k(999))
		if ok || v != nil {
			t.Fatalf("expected %v, got %v", nil, v)
		}
		if m.Len() != 0 {
			t.Fatalf("expected %v, got %v", 0, m.Len())
		}
		// set a bunch of items
		for i := 0; i < len(nums); i++ {
			v, ok := m.Set(nums[i], nums[i])
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
			v, ok := m.Get(nums[i])
			if !ok || v == "" || v != nums[i] {
				t.Fatalf("expected %v, got %v", nums[i], v)
			}
		}
		// replace all the items
		shuffle(nums)
		for i := 0; i < len(nums); i++ {
			v, ok := m.Set(nums[i], add(nums[i], 1))
			if !ok || v != nums[i] {
				t.Fatalf("expected %v, got %v", nums[i], v)
			}
		}
		if m.Len() != N {
			t.Fatalf("expected %v, got %v", N, m.Len())
		}
		// retrieve all the items
		shuffle(nums)
		for i := 0; i < len(nums); i++ {
			v, ok := m.Get(nums[i])
			if !ok || v != add(nums[i], 1) {
				t.Fatalf("expected %v, got %v", add(nums[i], 1), v)
			}
		}
		// remove half the items
		shuffle(nums)
		for i := 0; i < len(nums)/2; i++ {
			v, ok := m.Delete(nums[i])
			if !ok || v != add(nums[i], 1) {
				t.Fatalf("expected %v, got %v", add(nums[i], 1), v)
			}
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
			if !ok || v != add(nums[i], 1) {
				t.Fatalf("expected %v, got %v", add(nums[i], 1), v)
			}
		}
		// try to delete again, make sure they don't exist
		for i := 0; i < len(nums)/2; i++ {
			v, ok := m.Delete(nums[i])
			if ok || v != nil {
				t.Fatalf("expected %v, got %v", nil, v)
			}
		}
		if m.Len() != N/2 {
			t.Fatalf("expected %v, got %v", N/2, m.Len())
		}
		for k, v := range m.All() {
			if v != add(k, 1) {
				t.Fatalf("expected %v, got %v", add(k, 1), v)
			}
		}
		var n int
		for range m.All() {
			n++
			break
		}

		if n != 1 {
			t.Fatalf("expected %v, got %v", 1, n)
		}
		for i := len(nums) / 2; i < len(nums); i++ {
			v, ok := m.Delete(nums[i])
			if !ok || v != add(nums[i], 1) {
				t.Fatalf("expected %v, got %v", add(nums[i], 1), v)
			}
		}
	}
}

func TestCompareAndSwap(t *testing.T) {
	var m Map[string, string]
	m.IsEqual = func(old, new string) bool {
		return old == new
	}

	if !m.CompareAndSwap("hello", "", "world") {
		t.Fatal("TestCompareAndSwap: expected the first swap to succeed")
	}
	if v, ok := m.Get("hello"); !ok || v != "world" {
		t.Fatalf("TestCompareAndSwap: got %q, want %q", v, "world")
	}

	if !m.CompareAndSwap("hello", "world", "planet") {
		t.Fatal("TestCompareAndSwap: expected the second swap to succeed")
	}

	if v, ok := m.Get("hello"); !ok || v != "planet" {
		t.Fatalf("TestCompareAndSwap: got %q, want %q", v, "planet")
	}

	if m.CompareAndSwap("hello", "world", "planet") {
		t.Fatal("TestCompareAndSwap: expected the third swap to fail")
	}
}

func TestDeleteAccept(t *testing.T) {
	var m Map[string, string]
	m.IsEqual = func(old, new string) bool {
		return old == new
	}

	if !m.CompareAndDelete("hello", "world") {
		t.Fatal("TestDeleteAccept: expected the first delete to succeed")
	}

	m.Set("hello", "world")
	if swapped := m.CompareAndDelete("hello", "world"); !swapped {
		t.Fatal("TestDeleteAccept: expected the second delete to succeed")
	}

	if v, ok := m.Get("hello"); ok || v != "" {
		t.Fatalf("TestDeleteAccept: got %q, want %q", v, "")
	}

	m.Set("hello", "world")
	if m.CompareAndDelete("hello", "planet") {
		t.Fatal("TestDeleteAccept: expected the third delete to fail")
	}
}

func TestClear(t *testing.T) {
	var m Map[string, int]
	for i := 0; i < 1000; i++ {
		m.Set(fmt.Sprintf("%d", i), i)
	}
	if m.Len() != 1000 {
		t.Fatalf("expected '%v', got '%v'", 1000, m.Len())
	}
	m.Clear()
	if m.Len() != 0 {
		t.Fatalf("expected '%v', got '%v'", 0, m.Len())
	}

}
