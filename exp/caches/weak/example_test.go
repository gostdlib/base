package weak_test

import (
	"fmt"
	"runtime"
	"time"
	stdweak "weak"

	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/exp/caches/weak"
)

// User is a value stored in the cache for the examples below.
type User struct {
	ID   string
	Name string
}

// Example shows the basic Set/Get/Del lifecycle. Strong references to the stored values are held for the duration of
// the example (via runtime.KeepAlive), so the GC cannot reclaim the weakly held entry mid-example.
func Example() {
	ctx := context.Background()

	cache, err := weak.New[string, User](ctx, "users")
	if err != nil {
		panic(err)
	}

	alice := &User{ID: "u1", Name: "Alice"}
	if _, _, err := cache.Set(ctx, alice.ID, alice); err != nil {
		panic(err)
	}

	got, ok, err := cache.Get(ctx, "u1")
	if err != nil {
		panic(err)
	}
	fmt.Printf("found=%v name=%s\n", ok, got.Name)

	if _, _, err := cache.Del(ctx, "u1"); err != nil {
		panic(err)
	}
	_, ok, err = cache.Get(ctx, "u1")
	if err != nil {
		panic(err)
	}
	fmt.Printf("found=%v\n", ok)

	runtime.KeepAlive(alice)
	// Output:
	// found=true name=Alice
	// found=false
}

// ExampleWithFiller shows cache-aside loading: on a miss the filler loads the value from a backing "database" and the
// cache stores it. The db map holds strong references to the loaded values, so they stay live for the example.
func ExampleWithFiller() {
	ctx := context.Background()

	db := map[string]*User{"u1": {ID: "u1", Name: "Alice"}}
	filler := func(ctx context.Context, k string) (*User, bool, error) {
		u, ok := db[k]
		return u, ok, nil
	}

	cache, err := weak.New[string, User](ctx, "users", weak.WithFiller(filler))
	if err != nil {
		panic(err)
	}

	got, ok, err := cache.Get(ctx, "u1")
	if err != nil {
		panic(err)
	}
	fmt.Printf("hit=%v name=%s\n", ok, got.Name)

	_, ok, err = cache.Get(ctx, "missing")
	if err != nil {
		panic(err)
	}
	fmt.Printf("hit=%v\n", ok)
	// Output:
	// hit=true name=Alice
	// hit=false
}

// ExampleWithSetter shows write-through on Set: the setter persists the value to a backing "database" before the cache
// records it. If the setter returns an error, the value is not cached.
func ExampleWithSetter() {
	ctx := context.Background()

	db := map[string]*User{}
	setter := func(ctx context.Context, k string, v *User) error {
		db[k] = v
		return nil
	}

	cache, err := weak.New[string, User](ctx, "users", weak.WithSetter(setter))
	if err != nil {
		panic(err)
	}

	alice := &User{ID: "u1", Name: "Alice"}
	if _, _, err := cache.Set(ctx, alice.ID, alice); err != nil {
		panic(err)
	}
	fmt.Printf("db has u1: %s\n", db["u1"].Name)

	runtime.KeepAlive(alice)
	// Output:
	// db has u1: Alice
}

// ExampleWithDeleter shows write-through delete on Del: the deleter removes the value from the backing "database"
// before the in-memory delete. A deleter error aborts the delete and leaves the entry in place.
func ExampleWithDeleter() {
	ctx := context.Background()

	db := map[string]*User{"u1": {ID: "u1", Name: "Alice"}}
	deleter := func(ctx context.Context, k string) error {
		delete(db, k)
		return nil
	}

	cache, err := weak.New[string, User](ctx, "users", weak.WithDeleter[string](deleter))
	if err != nil {
		panic(err)
	}

	if _, _, err := cache.Del(ctx, "u1"); err != nil {
		panic(err)
	}
	_, ok := db["u1"]
	fmt.Printf("db still has u1: %v\n", ok)
	// Output:
	// db still has u1: false
}

// ExampleWithTTL shows construction with a time-to-live. WithTTL takes (ttl, maxTTL, interval): ttl is the minimum
// duration an entry is held with a strong reference (protecting it from GC), maxTTL forces eviction even if the value
// is still referenced (0 disables it), and interval is how often the background cleanup runs. The background cleanup
// loop runs on the context's task manager and stops when the context is canceled, so the example cancels on return to
// let it terminate cleanly. The Get here happens within the ttl hold window with a strong reference held, so it is
// deterministic.
func ExampleWithTTL() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache, err := weak.New[string, User](ctx, "users", weak.WithTTL(1*time.Minute, 5*time.Minute, 1*time.Minute))
	if err != nil {
		panic(err)
	}

	alice := &User{ID: "u1", Name: "Alice"}
	if _, _, err := cache.Set(ctx, alice.ID, alice); err != nil {
		panic(err)
	}

	got, ok, err := cache.Get(ctx, "u1")
	if err != nil {
		panic(err)
	}
	fmt.Printf("hit=%v name=%s\n", ok, got.Name)

	runtime.KeepAlive(alice)
	// Output:
	// hit=true name=Alice
}

// ExampleWithDeDupe shows de-duplication: two keys whose values compare equal under the less function share a single
// stored representative, so both keys' Get calls return the same pointer. The less function must tolerate a weak
// pointer whose Value() is nil (values can be collected before tree maintenance runs). Both values are held live for
// the example, so the shared-pointer result is deterministic.
func ExampleWithDeDupe() {
	ctx := context.Background()

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

	cache, err := weak.New[string, User](ctx, "users", weak.WithDeDupe(less))
	if err != nil {
		panic(err)
	}

	v1 := &User{ID: "u1", Name: "Alice"}
	v2 := &User{ID: "u1", Name: "Alice"} // Equal contents to v1.
	if _, _, err := cache.Set(ctx, "a", v1); err != nil {
		panic(err)
	}
	if _, _, err := cache.Set(ctx, "b", v2); err != nil {
		panic(err)
	}

	got1, _, err := cache.Get(ctx, "a")
	if err != nil {
		panic(err)
	}
	got2, _, err := cache.Get(ctx, "b")
	if err != nil {
		panic(err)
	}
	fmt.Printf("a=%s b=%s shared=%v\n", got1.Name, got2.Name, got1 == got2)

	runtime.KeepAlive(v1)
	runtime.KeepAlive(v2)
	// Output:
	// a=Alice b=Alice shared=true
}

// ExampleWithSingleFlight shows a filler paired with single-flight loading. Single-flight collapses concurrent Get
// calls for the same key into one filler invocation, preventing a thundering herd against the backing store. This
// sequential demo loads once and then serves the cached value on the second Get.
func ExampleWithSingleFlight() {
	ctx := context.Background()

	loads := 0
	db := map[string]*User{"u1": {ID: "u1", Name: "Alice"}}
	filler := func(ctx context.Context, k string) (*User, bool, error) {
		loads++
		u, ok := db[k]
		return u, ok, nil
	}

	cache, err := weak.New[string, User](ctx, "users", weak.WithFiller(filler), weak.WithSingleFlight())
	if err != nil {
		panic(err)
	}

	got, ok, err := cache.Get(ctx, "u1")
	if err != nil {
		panic(err)
	}
	fmt.Printf("hit=%v name=%s loads=%d\n", ok, got.Name, loads)
	// Output:
	// hit=true name=Alice loads=1
}
