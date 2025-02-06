package sync

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
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

	all := m.Map()
	if len(all) != 1000 {
		t.Errorf("expected map size 1000, got %d", len(all))
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

	if len(m.locks) != runtime.NumCPU() || len(m.maps) != len(m.locks) {
		t.Fatalf("TestShardedMapConcurrentAccess: you've got major problems, check this line")
	}
}
