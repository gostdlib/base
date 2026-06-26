package sync

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestShardedMap(t *testing.T) {
	m := ShardedMap[string, int]{}
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", i)
			m.Set(key, i)
			if val, ok := m.Get(key); !ok || val != i {
				t.Errorf("expected %d, got %d", i, val)
			}
		}(i)
	}
	wg.Wait()

	n := 0
	for range m.All() {
		n++
	}
	if n != 1000 {
		t.Errorf("expected map size 1000, got %d", n)
	}
}

func TestShardedMapConcurrentAccess(t *testing.T) {
	m := ShardedMap[string, int]{}

	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", i)
			m.Set(key, i)
		}(i)
	}

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", i)
			m.Get(key)
		}(i)
	}

	wg.Wait()
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
