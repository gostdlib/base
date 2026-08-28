package sync

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestShardedMap(t *testing.T) {
	m := ShardedMap[string, int]{}

	// Writers assert what they wrote. A concurrent reader wave runs against them so the race detector sees
	// readers and writers overlapping; the readers cannot assert, because their key may not be written yet.
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", i)
			m.Set(key, i)
			if val, ok := m.Get(key); !ok || val != i {
				t.Errorf("TestShardedMap: got %d, want %d", val, i)
			}
		}(i)
	}
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Get(fmt.Sprintf("key%d", i))
		}(i)
	}
	wg.Wait()

	n := 0
	for range m.All() {
		n++
	}
	if n != 1000 {
		t.Errorf("TestShardedMap: got map size %d, want 1000", n)
	}
}

func TestShardedMapSetAccept(t *testing.T) {
	const key = "key"

	tests := []struct {
		name string
		// existing, when non-empty, is set on the key before SetAccept is called.
		existing      string
		useAccept     bool
		acceptRet     bool
		wantPrev      string
		wantRepl      bool
		wantPrevSeen  string
		wantStateSeen bool
		wantVal       string
		wantExist     bool
	}{
		{
			name:      "Success: nil accept sets a new key",
			wantVal:   "new",
			wantExist: true,
		},
		{
			name:      "Success: nil accept replaces an existing key",
			existing:  "old",
			wantPrev:  "old",
			wantRepl:  true,
			wantVal:   "new",
			wantExist: true,
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
			wantPrevSeen:  "",
			wantStateSeen: false,
			wantExist:     false,
		},
		{
			name:          "Success: accept rejects a replacement so the old value is restored",
			existing:      "old",
			useAccept:     true,
			acceptRet:     false,
			wantPrevSeen:  "old",
			wantStateSeen: true,
			wantVal:       "old",
			wantExist:     true,
		},
	}

	for _, test := range tests {
		m := ShardedMap[string, string]{}
		if test.existing != "" {
			m.Set(key, test.existing)
		}

		var (
			called   bool
			gotPrev  string
			gotState bool
		)
		var fn func(prev string, replaced bool) bool
		if test.useAccept {
			fn = func(prev string, replaced bool) bool {
				called = true
				gotPrev = prev
				gotState = replaced
				return test.acceptRet
			}
		}

		prev, repl := m.SetAccept(key, "new", fn)
		switch {
		case prev != test.wantPrev:
			t.Errorf("TestShardedMapSetAccept(%s): got prev == %q, want %q", test.name, prev, test.wantPrev)
		case repl != test.wantRepl:
			t.Errorf("TestShardedMapSetAccept(%s): got replaced == %v, want %v", test.name, repl, test.wantRepl)
		}

		if test.useAccept {
			switch {
			case !called:
				t.Errorf("TestShardedMapSetAccept(%s): accept was not called", test.name)
			case gotPrev != test.wantPrevSeen:
				t.Errorf("TestShardedMapSetAccept(%s): accept saw prev == %q, want %q", test.name, gotPrev, test.wantPrevSeen)
			case gotState != test.wantStateSeen:
				t.Errorf("TestShardedMapSetAccept(%s): accept saw replaced == %v, want %v", test.name, gotState, test.wantStateSeen)
			}
		}

		got, ok := m.Get(key)
		switch {
		case ok != test.wantExist:
			t.Errorf("TestShardedMapSetAccept(%s): after call, key exists == %v, want %v", test.name, ok, test.wantExist)
		case got != test.wantVal:
			t.Errorf("TestShardedMapSetAccept(%s): after call, value == %q, want %q", test.name, got, test.wantVal)
		}
	}
}

func TestShardedMapDeleteAccept(t *testing.T) {
	const key = "key"

	tests := []struct {
		name string
		// existing, when non-empty, is set on the key before DeleteAccept is called.
		existing      string
		useAccept     bool
		acceptRet     bool
		wantPrev      string
		wantDel       bool
		wantPrevSeen  string
		wantStateSeen bool
		wantVal       string
		wantExist     bool
	}{
		{
			name:      "Success: nil accept deletes an existing key",
			existing:  "old",
			wantPrev:  "old",
			wantDel:   true,
			wantExist: false,
		},
		{
			name:      "Success: nil accept on a missing key reports not deleted",
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
			wantPrevSeen:  "old",
			wantStateSeen: true,
			wantVal:       "old",
			wantExist:     true,
		},
		{
			name:          "Success: accept rejects deleting a missing key leaves it absent",
			useAccept:     true,
			acceptRet:     false,
			wantPrevSeen:  "",
			wantStateSeen: false,
			wantExist:     false,
		},
	}

	for _, test := range tests {
		m := ShardedMap[string, string]{}
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
			t.Errorf("TestShardedMapDeleteAccept(%s): got prev == %q, want %q", test.name, prev, test.wantPrev)
		case del != test.wantDel:
			t.Errorf("TestShardedMapDeleteAccept(%s): got deleted == %v, want %v", test.name, del, test.wantDel)
		}

		if test.useAccept {
			switch {
			case !called:
				t.Errorf("TestShardedMapDeleteAccept(%s): accept was not called", test.name)
			case gotPrev != test.wantPrevSeen:
				t.Errorf("TestShardedMapDeleteAccept(%s): accept saw prev == %q, want %q", test.name, gotPrev, test.wantPrevSeen)
			case gotState != test.wantStateSeen:
				t.Errorf("TestShardedMapDeleteAccept(%s): accept saw deleted == %v, want %v", test.name, gotState, test.wantStateSeen)
			}
		}

		got, ok := m.Get(key)
		switch {
		case ok != test.wantExist:
			t.Errorf("TestShardedMapDeleteAccept(%s): after call, key exists == %v, want %v", test.name, ok, test.wantExist)
		case got != test.wantVal:
			t.Errorf("TestShardedMapDeleteAccept(%s): after call, value == %q, want %q", test.name, got, test.wantVal)
		}
	}
}

// TestShardedMapCompareAndDelete pins the contract callers rely on to claim ownership of a key:
// exactly one caller may win a delete, and losing must be reported as losing. The missing-key case
// is a regression guard — it used to report success, which inverts the primitive for precisely the
// situation it exists for. A caller doing "delete my entry, and only act if I removed it" would be
// told it won every time the entry was already gone, so every late caller would act too. When the
// action is not idempotent, that is a correctness bug the caller cannot defend against.
func TestShardedMapCompareAndDelete(t *testing.T) {
	const key = "key"

	tests := []struct {
		name string
		// existing, when non-empty, is set on the key before the call.
		existing string
		old      string
		wantDel  bool
		// wantVal and wantExist describe the key after the call.
		wantVal   string
		wantExist bool
	}{
		{
			name:     "Success: a matching value is deleted",
			existing: "old",
			old:      "old",
			wantDel:  true,
		},
		{
			name:    "Success: a missing key reports not deleted",
			old:     "old",
			wantDel: false,
		},
		{
			name:      "Success: a mismatched value is left in place",
			existing:  "other",
			old:       "old",
			wantDel:   false,
			wantVal:   "other",
			wantExist: true,
		},
	}

	for _, test := range tests {
		m := ShardedMap[string, string]{IsEqual: func(a, b string) bool { return a == b }}
		if test.existing != "" {
			m.Set(key, test.existing)
		}

		if got := m.CompareAndDelete(key, test.old); got != test.wantDel {
			t.Errorf("TestShardedMapCompareAndDelete(%s): got deleted == %v, want %v", test.name, got, test.wantDel)
		}

		got, ok := m.Get(key)
		switch {
		case ok != test.wantExist:
			t.Errorf("TestShardedMapCompareAndDelete(%s): after call, key exists == %v, want %v", test.name, ok, test.wantExist)
		case got != test.wantVal:
			t.Errorf("TestShardedMapCompareAndDelete(%s): after call, value == %q, want %q", test.name, got, test.wantVal)
		}
	}
}

// TestShardedMapCompareAndDeleteOnlyOneWinner is the property the missing-key case protects: with
// many goroutines racing to delete the same entry, exactly one may be told it won.
func TestShardedMapCompareAndDeleteOnlyOneWinner(t *testing.T) {
	const (
		key      = "key"
		val      = "val"
		builders = 50
	)

	m := ShardedMap[string, string]{IsEqual: func(a, b string) bool { return a == b }}
	m.Set(key, val)

	var wins atomic.Int64
	start := make(chan struct{})
	done := make(chan struct{})
	for i := 0; i < builders; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			<-start
			if m.CompareAndDelete(key, val) {
				wins.Add(1)
			}
		}()
	}
	close(start)
	for i := 0; i < builders; i++ {
		<-done
	}

	if got := wins.Load(); got != 1 {
		t.Errorf("TestShardedMapCompareAndDeleteOnlyOneWinner: %d goroutines were told they won the delete, want 1", got)
	}
}

func TestShardedMapAllLocked(t *testing.T) {
	m := ShardedMap[string, int]{}
	const n = 1000
	for i := 0; i < n; i++ {
		m.Set(fmt.Sprintf("key%d", i), i)
	}

	// Other goroutines reading concurrently must be safe while we iterate under WithLock(); run under
	// -race to catch any unsynchronized access.
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Get(fmt.Sprintf("key%d", i))
		}(i)
	}

	seen := 0
	for range m.All(WithLock()) {
		seen++
	}
	wg.Wait()

	if seen != n {
		t.Errorf("TestShardedMapAllLocked: iterated %d entries, want %d", seen, n)
	}
}

func TestShardedMapAllLockedReleasesLock(t *testing.T) {
	tests := []struct {
		name string
		// stop ends a WithLock() iteration early in a particular way; afterwards the shard lock must
		// have been released or subsequent writes would deadlock.
		stop func(m *ShardedMap[string, int])
	}{
		{
			name: "Success: breaking out of the iteration releases the shard lock",
			stop: func(m *ShardedMap[string, int]) {
				for range m.All(WithLock()) {
					break
				}
			},
		},
		{
			name: "Success: a panic in the iteration releases the shard lock",
			stop: func(m *ShardedMap[string, int]) {
				defer func() { _ = recover() }()
				for range m.All(WithLock()) {
					panic("boom")
				}
			},
		},
	}

	for _, test := range tests {
		m := ShardedMap[string, int]{}
		for i := 0; i < 100; i++ {
			m.Set(fmt.Sprintf("key%d", i), i)
		}

		test.stop(&m)

		// If a shard lock leaked, re-writing every key (which touches the leaked shard) would deadlock.
		done := make(chan struct{})
		go func() {
			for i := 0; i < 100; i++ {
				m.Set(fmt.Sprintf("key%d", i), i*2)
			}
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("TestShardedMapAllLockedReleasesLock(%s): writes deadlocked; the iteration leaked a shard lock", test.name)
		}
	}
}

// TestShardedMapCompareAndDeleteLateIsEqual is the regression test for the shared sync.Once freezing IsEqual.
// SetAccept and DeleteAccept do not need IsEqual, but they used to run the same once.Do, so calling either before
// assigning IsEqual permanently cached nil and every later compare operation panicked even though IsEqual was set.
func TestShardedMapCompareAndDeleteLateIsEqual(t *testing.T) {
	tests := []struct {
		name string
		burn func(m *ShardedMap[string, string])
	}{
		{
			name: "Success: IsEqual assigned before any other call",
			burn: func(m *ShardedMap[string, string]) {},
		},
		{
			name: "Success: IsEqual assigned after SetAccept",
			burn: func(m *ShardedMap[string, string]) { m.SetAccept("hello", "world", nil) },
		},
		{
			name: "Success: IsEqual assigned after DeleteAccept",
			burn: func(m *ShardedMap[string, string]) { m.DeleteAccept("nothing", nil) },
		},
	}

	for _, test := range tests {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("TestShardedMapCompareAndDeleteLateIsEqual(%s): got panic == %v, want no panic", test.name, r)
				}
			}()

			m := &ShardedMap[string, string]{}
			test.burn(m)

			m.IsEqual = func(a, b string) bool { return a == b }
			m.Set("hello", "world")

			if !m.CompareAndDelete("hello", "world") {
				t.Errorf("TestShardedMapCompareAndDeleteLateIsEqual(%s): got deleted == false, want true", test.name)
			}
		}()
	}
}
