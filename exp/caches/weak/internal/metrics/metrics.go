package metrics

import "go.opentelemetry.io/otel/metric"

// Cache contains metrics for a cache.
type Cache struct {
	meter metric.Meter
	// CacheItems is the number of items currently stored in the cache.
	CacheItems metric.Int64UpDownCounter
	// CacheHits is the number of cache hits.
	CacheHits metric.Int64Counter
	// CacheMisses is the number of cache misses.
	CacheMisses metric.Int64Counter
	// Fills is the number of times an item was added to the cache from a Filler.
	Fills metric.Int64Counter
	// Dedups is the number of times an item at a new key was found in the dedup btree.
	// This indicates that an item was reused.
	Dedups metric.Int64Counter
}

// New creates a new Cache metrics instance.
func New(m metric.Meter) *Cache {
	metrics := &Cache{meter: m}

	var err error
	metrics.CacheItems, err = m.Int64UpDownCounter("cache_items", metric.WithDescription("The number of items currently stored in the cache. This is an approximation due to the nature of weak references."))
	if err != nil {
		panic(err)
	}
	metrics.CacheHits, err = m.Int64Counter("cache_hits", metric.WithDescription("The number of cache hits."))
	if err != nil {
		panic(err)
	}
	metrics.CacheMisses, err = m.Int64Counter("cache_misses", metric.WithDescription("The number of cache misses."))
	if err != nil {
		panic(err)
	}
	metrics.Fills, err = m.Int64Counter("fills", metric.WithDescription("The number of times an item was added to the cache from a Filler."))
	if err != nil {
		panic(err)
	}
	metrics.Dedups, err = m.Int64Counter("dedups", metric.WithDescription("The number of times an item at a new key was found in the dedup btree. This indicates that an item was reused."))
	if err != nil {
		panic(err)
	}

	return metrics
}
