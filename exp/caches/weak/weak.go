// Package weak provides a thread-safe weak pointer cache that automatically cleans up entries
// when the weakly referenced objects are garbage collected. It supports basic operations like Get, Set and Del.
// The internal implementation uses sharded maps for concurrency and performance.
// The cache has no size limit and relies on Go's runtime to manage memory once objects are no longer referenced.
// This can be used to implement caches on top of more durable storage layers via the filler, setter, and deleter functions.
// You can then wrap calls for database Set(), Get(), and Delete() operations with this cache to improve performance
// while keeping memory usage in check via weak references.
package weak

import (
	"fmt"
	"runtime"
	"time"
	"weak"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/context"
	"github.com/gostdlib/base/exp/caches/weak/internal/hashmap"
	cacheMetrics "github.com/gostdlib/base/exp/caches/weak/internal/metrics"
	"github.com/gostdlib/base/exp/caches/weak/internal/shardmap"
	"github.com/gostdlib/base/telemetry/otel/metrics"
)

type ttlEntry[V any] struct {
	hold  time.Time
	value *V
}

// Cache is a weak pointer cache.
type Cache[K comparable, V any] struct {
	m          *shardmap.Map[K, V]
	ttl        time.Duration
	interval   time.Duration
	useFlights bool
	getFlight  sync.Flight[K, struct{}]

	ttlLock sync.Mutex
	ttlMap  hashmap.Map[K, ttlEntry[V]]

	filler  Filler[K, V]
	setter  Setter[K, V]
	deleter Deleter[K]

	metrics *cacheMetrics.Cache
}

type opts struct {
	ttl        time.Duration
	interval   time.Duration
	useFlights bool
	less       any
	// filler, setter and deleter are of type any to avoid always using generics on opts.
	// They will be type asserted when used.
	filler  any
	setter  any
	deleter any
}

// Option is an option for New().
type Option func(o opts) (opts, error)

// WithTTL sets the time-to-live for entries in the cache and the cleanup interval.
// Entries older than ttl will be removed during cleanup.
// The cleanup interval must be at least 1 second.
// If ttl is 0, an error is returned.
func WithTTL(ttl, interval time.Duration) Option {
	return func(o opts) (opts, error) {
		if interval < 1*time.Second {
			return o, fmt.Errorf("cleanup interval must be at least 1 second")
		}
		if ttl == 0 {
			return o, fmt.Errorf("ttl must be greater than 0")
		}
		o.ttl = ttl
		o.interval = interval
		return o, nil
	}
}

// WithSingleFlight enables the use of the singleflight package for Get operations.
// This adds another lock on Get operations, but prevents multiple concurrent
// Get() calls for the same key from causing multiple loads of the same value. Use this to
// prevent thundering herd problems when loading values from the cache. If not using WithFiller() to
// retrieve missing values, this option will likely slow operations down instead of speeding them up.
func WithSingleFlight[K comparable, V any]() Option {
	return func(o opts) (opts, error) {
		o.useFlights = true
		return o, nil
	}
}

// Filler is a function that will be run if the cache needs to fill a missing value. If this function
// returns a value, it will be set in the cache and returned to the caller. If it returns an error, the Get call will fail with
// that error.
type Filler[K comparable, V any] = shardmap.Filler[K, V]

// WithFiller sets a custom filler function for the cache. This is used to load missing values into the cache on Get calls.
func WithFiller[K comparable, V any](f Filler[K, V]) Option {
	return func(o opts) (opts, error) {
		o.filler = f
		return o, nil
	}
}

// Setter is a function that will be run when setting a value in the cache. If this function
// returns an error, the value will not be set. This is used to set values in durable storage when they are added to the cache.
type Setter[K comparable, V any] = shardmap.Setter[K, V]

// WithSetter sets a custom setter function for the cache. This is used to set values in durable storage when they are added to the cache.
func WithSetter[K comparable, V any](s Setter[K, V]) Option {
	return func(o opts) (opts, error) {
		o.setter = s
		return o, nil
	}
}

// Deleter is a function that will be run when deleting a value from the cache. If this function
// returns an error, the value will not be deleted. This is used to delete values from durable storage when they are removed
// from the cache.
type Deleter[K comparable] = shardmap.Deleter[K]

// WithDeleter sets a custom deleter function for the cache. This is used to delete values from durable storage when they
// are removed from the cache.
func WithDeleter[K comparable](d Deleter[K]) Option {
	return func(o opts) (opts, error) {
		o.deleter = d
		return o, nil
	}
}

// WithDeDupe enables de-duplication of values in the cache based on the provided less function.
// When enabled, the cache will ensure that only one instance of a value exists in the cache
// for any given key, based on the comparison provided by the less function.
// This can help reduce memory usage when many identical values are stored in the cache.
func WithDeDupe[V any](less func(a, b weak.Pointer[V]) bool) Option {
	return func(o opts) (opts, error) {
		o.less = less
		return o, nil
	}
}

func fillMetricWrap[K comparable, V any](metrics *cacheMetrics.Cache, f Filler[K, V]) Filler[K, V] {
	return func(ctx context.Context, k K) (*V, bool, error) {
		v, ok, err := f(ctx, k)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, nil
		}
		metrics.Fills.Add(ctx, 1)
		return v, true, nil
	}
}

// New creates a new Cache with the given options. Name is a unique identifier for the cache, used for metrics.
func New[K comparable, V any](ctx context.Context, name string, options ...Option) (*Cache[K, V], error) {
	if name == "" {
		return nil, fmt.Errorf("name cannot be empty")
	}
	o := opts{}
	for _, option := range options {
		var err error
		o, err = option(o)
		if err != nil {
			return nil, err
		}
	}

	mp := context.MeterProvider(ctx)
	meter := mp.Meter(metrics.MeterName(2) + "/" + name)
	cm := cacheMetrics.New(meter)

	var m *shardmap.Map[K, V]
	if o.less != nil {
		m = shardmap.New[K, V](o.less.(func(a, b weak.Pointer[V]) bool), cm)
	} else {
		m = shardmap.New[K, V](nil, cm)
	}

	c := &Cache[K, V]{
		m:          m,
		useFlights: o.useFlights,
		metrics:    cm,
	}
	if o.filler != nil {
		c.filler = fillMetricWrap(c.metrics, o.filler.(Filler[K, V]))
	}
	if o.setter != nil {
		c.setter = o.setter.(Setter[K, V])
	}
	if o.deleter != nil {
		c.deleter = o.deleter.(Deleter[K])
	}
	if o.ttl > 0 {
		c.ttl = o.ttl
		c.interval = o.interval
		_ = context.Pool(ctx).Submit(
			ctx,
			func() {
				c.ttlExpire(ctx)
			},
		)
	}

	return c, nil
}

// ttlExpire runs in a background goroutine to clean up expired entries in the ttlMap.
// This map is holding values with a regular pointer to prevent the weak reference from
// being collected before the ttl expires. Once the TTL expires, the entry is deleted from the ttlMap,
// which allows the weak reference in the main map to be collected by the GC if not used.
func (m *Cache[K, V]) ttlExpire(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	deletions := []K{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			m.ttlLock.Lock()
			for k, v := range m.ttlMap.All() {
				if v.hold.Before(now) {
					deletions = append(deletions, k)
				}
			}
			for _, k := range deletions {
				m.ttlMap.Delete(k)
			}
			m.ttlLock.Unlock()
			deletions = deletions[:0]
		}
	}
}

// Set assigns a value to a key.
// Returns the previous value, or false when no value was assigned. If you
// Set a nil value, it is equivalent to Delete.
func (m *Cache[K, V]) Set(ctx context.Context, k K, v *V) (prev *V, replaced bool, err error) {
	if v == nil {
		prev, deleted, err := m.Del(ctx, k)
		if deleted {
			m.metrics.CacheItems.Add(ctx, -1)
		}
		return prev, deleted, err
	}

	return m.set(ctx, k, v)
}

func (m *Cache[K, V]) set(ctx context.Context, k K, v *V) (prev *V, replaced bool, err error) {
	if m.ttl > 0 {
		m.ttlLock.Lock()
		m.ttlMap.Set(k, ttlEntry[V]{hold: time.Now().Add(m.ttl), value: v})
		m.ttlLock.Unlock()
	}

	prev, replaced, err = m.m.Set(ctx, k, v, m.setter)
	if err != nil {
		return nil, false, err
	}
	runtime.AddCleanup[V, K](
		v,
		func(k K) {
			m.m.DeleteIfNil(k)
		},
		k,
	)

	if !replaced {
		return nil, false, nil
	}
	m.metrics.CacheItems.Add(ctx, 1)

	return prev, replaced, nil
}

// Get returns a value for a key.
// Returns false when no value has been assign for key.
func (m *Cache[K, V]) Get(ctx context.Context, k K) (value *V, ok bool, err error) {
	if m.useFlights {
		_, _, _ = m.getFlight.Do(
			ctx,
			k,
			func() (struct{}, error) {
				value, ok, err = m.m.Get(ctx, k, m.filler)
				return struct{}{}, nil // We don't need the value.
			},
		)
		return value, ok, err
	}
	if ok {
		m.metrics.CacheHits.Add(ctx, 1)
	} else {
		m.metrics.CacheMisses.Add(ctx, 1)
	}
	return m.m.Get(ctx, k, m.filler)
}

// Del deletes a value for a key.
// Returns the deleted value, or false when no value was assigned.
func (m *Cache[K, V]) Del(ctx context.Context, k K) (prev *V, deleted bool, err error) {
	if m.ttl > 0 {
		m.ttlLock.Lock()
		m.ttlMap.Delete(k)
		m.ttlLock.Unlock()
	}

	prev, deleted, err = m.m.Delete(ctx, k, m.deleter)
	if deleted {
		m.metrics.CacheItems.Add(ctx, -1)
	}
	return prev, deleted, err
}

// Len returns the number of values in map. This is an approximation since keys may hold nil values that
// have not yet been cleaned up.
func (m *Cache[K, V]) Len() int {
	return m.m.Len()
}
