package weak_test

import (
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	stdweak "weak"

	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/exp/caches/weak"
)

// benchKeys is the size of the pre-generated key/value pools used by the benchmarks. Keys are indexed with i%benchKeys
// so the hot loop never calls strconv/fmt. Every value pool is held alive for the whole benchmark (a strong []*User
// plus a final runtime.KeepAlive) so the GC cannot reclaim the weakly held entries mid-run and pollute timing.
const benchKeys = 1024

// benchPool builds a pool of keys and matching *User values. When sameContent is true every value has identical
// contents (for the de-dupe benchmark, so they collapse onto one representative); otherwise each value is distinct.
func benchPool(sameContent bool) (keys []string, users []*User) {
	keys = make([]string, benchKeys)
	users = make([]*User, benchKeys)
	for i := range keys {
		keys[i] = "key-" + strconv.Itoa(i)
		if sameContent {
			users[i] = &User{ID: "same", Name: "same"}
			continue
		}
		users[i] = &User{ID: keys[i], Name: "Alice"}
	}
	return keys, users
}

// BenchmarkSetGetDel measures the core no-options hot path: Set stores a value, Get reads a live cached value, and Del
// removes it. Del seeds a value under a stopped timer each iteration so only the delete is measured.
func BenchmarkSetGetDel(b *testing.B) {
	ctx := context.Background()
	keys, users := benchPool(false)

	b.Run("Set", func(b *testing.B) {
		cache, err := weak.New[string, User](ctx, "bench-set")
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, _, err := cache.Set(ctx, keys[i%benchKeys], users[i%benchKeys]); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Get", func(b *testing.B) {
		cache, err := weak.New[string, User](ctx, "bench-get")
		if err != nil {
			b.Fatal(err)
		}
		for i := range keys {
			if _, _, err := cache.Set(ctx, keys[i], users[i]); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, _, err := cache.Get(ctx, keys[i%benchKeys]); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Del", func(b *testing.B) {
		cache, err := weak.New[string, User](ctx, "bench-del")
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			k := keys[i%benchKeys]
			if _, _, err := cache.Set(ctx, k, users[i%benchKeys]); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			if _, _, err := cache.Del(ctx, k); err != nil {
				b.Fatal(err)
			}
		}
	})

	runtime.KeepAlive(users)
}

// BenchmarkFillerHit measures the cached-hit path with a filler configured: after warming the entry once, every Get
// hits the live cached value and never invokes the filler. The db map holds a strong reference so the entry stays live.
func BenchmarkFillerHit(b *testing.B) {
	ctx := context.Background()
	db := map[string]*User{"u1": {ID: "u1", Name: "Alice"}}
	filler := func(ctx context.Context, k string) (*User, bool, error) {
		u, ok := db[k]
		return u, ok, nil
	}

	cache, err := weak.New[string, User](ctx, "bench-filler-hit", weak.WithFiller(filler))
	if err != nil {
		b.Fatal(err)
	}
	if _, _, err := cache.Get(ctx, "u1"); err != nil { // Warm the cache so the loop measures hits.
		b.Fatal(err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := cache.Get(ctx, "u1"); err != nil {
			b.Fatal(err)
		}
	}
	runtime.KeepAlive(db)
}

// BenchmarkFillerMiss measures the miss path through the filler: the key is never in the backing db, so every Get is a
// miss that dispatches to the filler, which reports not-found and caches nothing. Each iteration exercises the filler.
func BenchmarkFillerMiss(b *testing.B) {
	ctx := context.Background()
	db := map[string]*User{}
	filler := func(ctx context.Context, k string) (*User, bool, error) {
		u, ok := db[k]
		return u, ok, nil
	}

	cache, err := weak.New[string, User](ctx, "bench-filler-miss", weak.WithFiller(filler))
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := cache.Get(ctx, "missing"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWithSetter measures Set through a write-through setter that persists to a backing db map before the cache
// records the value.
func BenchmarkWithSetter(b *testing.B) {
	ctx := context.Background()
	keys, users := benchPool(false)
	db := make(map[string]*User, benchKeys)
	setter := func(ctx context.Context, k string, v *User) error {
		db[k] = v
		return nil
	}

	cache, err := weak.New[string, User](ctx, "bench-setter", weak.WithSetter(setter))
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := cache.Set(ctx, keys[i%benchKeys], users[i%benchKeys]); err != nil {
			b.Fatal(err)
		}
	}
	runtime.KeepAlive(users)
}

// BenchmarkWithDeleter measures Del through a write-through deleter that removes the value from a backing db map before
// the in-memory delete. Each iteration seeds a value under a stopped timer so only the deleting Del is measured.
func BenchmarkWithDeleter(b *testing.B) {
	ctx := context.Background()
	keys, users := benchPool(false)
	db := make(map[string]*User, benchKeys)
	deleter := func(ctx context.Context, k string) error {
		delete(db, k)
		return nil
	}

	cache, err := weak.New[string, User](ctx, "bench-deleter", weak.WithDeleter[string](deleter))
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		k := keys[i%benchKeys]
		db[k] = users[i%benchKeys]
		if _, _, err := cache.Set(ctx, k, users[i%benchKeys]); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, _, err := cache.Del(ctx, k); err != nil {
			b.Fatal(err)
		}
	}
	runtime.KeepAlive(users)
}

// BenchmarkWithTTL measures Set and Get with TTL bookkeeping enabled (the ttlMap/ttlLock overhead the no-TTL path
// avoids). The ttl/maxTTL/interval are all long so the background cleanup tick never fires during the run, and the
// context is canceled in cleanup so the cleanup task terminates.
func BenchmarkWithTTL(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	keys, users := benchPool(false)

	newCache := func(name string) *weak.Cache[string, User] {
		cache, err := weak.New[string, User](ctx, name, weak.WithTTL(1*time.Hour, 24*time.Hour, 1*time.Hour))
		if err != nil {
			b.Fatal(err)
		}
		return cache
	}

	b.Run("Set", func(b *testing.B) {
		cache := newCache("bench-ttl-set")
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, _, err := cache.Set(ctx, keys[i%benchKeys], users[i%benchKeys]); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Get", func(b *testing.B) {
		cache := newCache("bench-ttl-get")
		for i := range keys {
			if _, _, err := cache.Set(ctx, keys[i], users[i]); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, _, err := cache.Get(ctx, keys[i%benchKeys]); err != nil {
				b.Fatal(err)
			}
		}
	})

	runtime.KeepAlive(users)
}

// BenchmarkWithDeDupe measures Set into a de-duping cache: distinct keys carry values that all compare equal under the
// less function, so every Set past the first collapses onto the single stored representative, exercising the dedup tree.
func BenchmarkWithDeDupe(b *testing.B) {
	ctx := context.Background()
	keys, users := benchPool(true)
	less := func(a, b stdweak.Pointer[User]) bool {
		av, bv := a.Value(), b.Value()
		switch {
		case av == nil && bv == nil:
			return false
		case av == nil:
			return true
		case bv == nil:
			return false
		case av.Name != bv.Name:
			return av.Name < bv.Name
		default:
			return av.ID < bv.ID
		}
	}

	cache, err := weak.New[string, User](ctx, "bench-dedupe", weak.WithDeDupe(less))
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := cache.Set(ctx, keys[i%benchKeys], users[i%benchKeys]); err != nil {
			b.Fatal(err)
		}
	}
	runtime.KeepAlive(users)
}

// BenchmarkWithSingleFlight measures concurrent Gets on a single key through single-flight, the option's whole purpose:
// b.RunParallel fans many goroutines at one key while single-flight collapses the concurrent misses into a minimal
// number of filler loads. The load counter records how many times the filler actually ran.
func BenchmarkWithSingleFlight(b *testing.B) {
	ctx := context.Background()
	db := map[string]*User{"u1": {ID: "u1", Name: "Alice"}}
	var loads atomic.Int64
	filler := func(ctx context.Context, k string) (*User, bool, error) {
		loads.Add(1)
		u, ok := db[k]
		return u, ok, nil
	}

	cache, err := weak.New[string, User](ctx, "bench-singleflight", weak.WithFiller(filler), weak.WithSingleFlight())
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, _, err := cache.Get(ctx, "u1"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.ReportMetric(float64(loads.Load()), "loads")
	runtime.KeepAlive(db)
}
