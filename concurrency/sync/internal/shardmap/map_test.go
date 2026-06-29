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

func TestCompareAndDelete(t *testing.T) {
	var m Map[string, string]
	m.IsEqual = func(old, new string) bool {
		return old == new
	}

	if !m.CompareAndDelete("hello", "world") {
		t.Fatal("TestCompareAndDelete: expected the first delete to succeed")
	}

	m.Set("hello", "world")
	if swapped := m.CompareAndDelete("hello", "world"); !swapped {
		t.Fatal("TestCompareAndDelete: expected the second delete to succeed")
	}

	if v, ok := m.Get("hello"); ok || v != "" {
		t.Fatalf("TestCompareAndDelete: got %q, want %q", v, "")
	}

	m.Set("hello", "world")
	if m.CompareAndDelete("hello", "planet") {
		t.Fatal("TestCompareAndDelete: expected the third delete to fail")
	}
}

func TestSetAccept(t *testing.T) {
	const key = "key"

	// accept reports the value passed to a non-nil accept func so the test can assert on it.
	type accept struct {
		ret      bool
		gotPrev  string
		gotState bool
		called   bool
	}

	tests := []struct {
		name string
		// existing, when non-empty, is set on the key before SetAccept is called.
		existing string
		// useAccept controls whether a non-nil accept func is passed.
		useAccept bool
		// acceptRet is the value the accept func returns when useAccept is true.
		acceptRet bool
		wantPrev  string
		wantRepl  bool
		// wantPrevSeen / wantStateSeen are what accept should have been handed.
		wantPrevSeen  string
		wantStateSeen bool
		// wantVal / wantExist describe the key's state after the call.
		wantVal   string
		wantExist bool
	}{
		{
			name:      "Success: nil accept sets a new key",
			useAccept: false,
			wantPrev:  "",
			wantRepl:  false,
			wantVal:   "new",
			wantExist: true,
		},
		{
			name:      "Success: nil accept replaces an existing key",
			existing:  "old",
			useAccept: false,
			wantPrev:  "old",
			wantRepl:  true,
			wantVal:   "new",
			wantExist: true,
		},
		{
			name:          "Success: accept returns true and keeps a new key",
			useAccept:     true,
			acceptRet:     true,
			wantPrev:      "",
			wantRepl:      false,
			wantPrevSeen:  "",
			wantStateSeen: false,
			wantVal:       "new",
			wantExist:     true,
		},
		{
			name:          "Success: accept returns true and keeps a replacement",
			existing:      "old",
			useAccept:     true,
			acceptRet:     true,
			wantPrev:      "old",
			wantRepl:      true,
			wantPrevSeen:  "old",
			wantStateSeen: true,
			wantVal:       "new",
			wantExist:     true,
		},
		{
			name:          "Success: accept rejects a new key so it is deleted",
			useAccept:     true,
			acceptRet:     false,
			wantPrev:      "",
			wantRepl:      false,
			wantPrevSeen:  "",
			wantStateSeen: false,
			wantVal:       "",
			wantExist:     false,
		},
		{
			name:          "Success: accept rejects a replacement so the old value is restored",
			existing:      "old",
			useAccept:     true,
			acceptRet:     false,
			wantPrev:      "",
			wantRepl:      false,
			wantPrevSeen:  "old",
			wantStateSeen: true,
			wantVal:       "old",
			wantExist:     true,
		},
	}

	for _, test := range tests {
		var m Map[string, string]
		if test.existing != "" {
			m.Set(key, test.existing)
		}

		var a accept
		a.ret = test.acceptRet
		var fn func(prev string, replaced bool) bool
		if test.useAccept {
			fn = func(prev string, replaced bool) bool {
				a.called = true
				a.gotPrev = prev
				a.gotState = replaced
				return a.ret
			}
		}

		prev, repl := m.SetAccept(key, "new", fn)
		switch {
		case prev != test.wantPrev:
			t.Errorf("TestSetAccept(%s): got prev == %q, want %q", test.name, prev, test.wantPrev)
		case repl != test.wantRepl:
			t.Errorf("TestSetAccept(%s): got replaced == %v, want %v", test.name, repl, test.wantRepl)
		}

		if test.useAccept {
			switch {
			case !a.called:
				t.Errorf("TestSetAccept(%s): accept was not called", test.name)
			case a.gotPrev != test.wantPrevSeen:
				t.Errorf("TestSetAccept(%s): accept saw prev == %q, want %q", test.name, a.gotPrev, test.wantPrevSeen)
			case a.gotState != test.wantStateSeen:
				t.Errorf("TestSetAccept(%s): accept saw replaced == %v, want %v", test.name, a.gotState, test.wantStateSeen)
			}
		}

		got, ok := m.Get(key)
		switch {
		case ok != test.wantExist:
			t.Errorf("TestSetAccept(%s): after call, key exists == %v, want %v", test.name, ok, test.wantExist)
		case got != test.wantVal:
			t.Errorf("TestSetAccept(%s): after call, value == %q, want %q", test.name, got, test.wantVal)
		}
	}
}

func TestDeleteAccept(t *testing.T) {
	const key = "key"

	tests := []struct {
		name string
		// existing, when non-empty, is set on the key before DeleteAccept is called.
		existing  string
		useAccept bool
		acceptRet bool
		wantPrev  string
		wantDel   bool
		// wantPrevSeen / wantStateSeen are what accept should have been handed.
		wantPrevSeen  string
		wantStateSeen bool
		// wantVal / wantExist describe the key's state after the call.
		wantVal   string
		wantExist bool
	}{
		{
			name:      "Success: nil accept deletes an existing key",
			existing:  "old",
			useAccept: false,
			wantPrev:  "old",
			wantDel:   true,
			wantExist: false,
		},
		{
			name:      "Success: nil accept on a missing key reports not deleted",
			useAccept: false,
			wantPrev:  "",
			wantDel:   false,
			wantExist: false,
		},
		{
			name:          "Success: accept returns true and keeps the deletion",
			existing:      "old",
			useAccept:     true,
			acceptRet:     true,
			wantPrev:      "old",
			wantDel:       true,
			wantPrevSeen:  "old",
			wantStateSeen: true,
			wantExist:     false,
		},
		{
			name:          "Success: accept rejects so the existing key is restored",
			existing:      "old",
			useAccept:     true,
			acceptRet:     false,
			wantPrev:      "",
			wantDel:       false,
			wantPrevSeen:  "old",
			wantStateSeen: true,
			wantVal:       "old",
			wantExist:     true,
		},
		{
			name:          "Success: accept rejects deleting a missing key leaves it absent",
			useAccept:     true,
			acceptRet:     false,
			wantPrev:      "",
			wantDel:       false,
			wantPrevSeen:  "",
			wantStateSeen: false,
			wantExist:     false,
		},
	}

	for _, test := range tests {
		var m Map[string, string]
		if test.existing != "" {
			m.Set(key, test.existing)
		}

		var (
			called   bool
			gotPrev  string
			gotState bool
		)
		var fn func(prev string, deleted bool) bool
		if test.useAccept {
			fn = func(prev string, deleted bool) bool {
				called = true
				gotPrev = prev
				gotState = deleted
				return test.acceptRet
			}
		}

		prev, del := m.DeleteAccept(key, fn)
		switch {
		case prev != test.wantPrev:
			t.Errorf("TestDeleteAccept(%s): got prev == %q, want %q", test.name, prev, test.wantPrev)
		case del != test.wantDel:
			t.Errorf("TestDeleteAccept(%s): got deleted == %v, want %v", test.name, del, test.wantDel)
		}

		if test.useAccept {
			switch {
			case !called:
				t.Errorf("TestDeleteAccept(%s): accept was not called", test.name)
			case gotPrev != test.wantPrevSeen:
				t.Errorf("TestDeleteAccept(%s): accept saw prev == %q, want %q", test.name, gotPrev, test.wantPrevSeen)
			case gotState != test.wantStateSeen:
				t.Errorf("TestDeleteAccept(%s): accept saw deleted == %v, want %v", test.name, gotState, test.wantStateSeen)
			}
		}

		got, ok := m.Get(key)
		switch {
		case ok != test.wantExist:
			t.Errorf("TestDeleteAccept(%s): after call, key exists == %v, want %v", test.name, ok, test.wantExist)
		case got != test.wantVal:
			t.Errorf("TestDeleteAccept(%s): after call, value == %q, want %q", test.name, got, test.wantVal)
		}
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
