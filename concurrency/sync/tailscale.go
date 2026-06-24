package sync

// The items here are borrowed from tailscale sync.

import (
	"context"
	"sync/atomic"
)

// AssertLocked panics if m is not locked.
func AssertLocked(m *Mutex) {
	if m.TryLock() {
		m.Unlock()
		panic("mutex is not locked")
	}
}

// AssertRLocked panics if rw is not locked for reading or writing.
func AssertRLocked(rw *RWMutex) {
	if rw.TryLock() {
		rw.Unlock()
		panic("mutex is not locked")
	}
}

// AssertWLocked panics if rw is not locked for writing.
func AssertWLocked(rw *RWMutex) {
	if rw.TryRLock() {
		rw.RUnlock()
		panic("mutex is not rlocked")
	}
}

// AtomicValue is the generic version of [atomic.Value].
// See [MutexValue] for guidance on whether to use this type.
type AtomicValue[T any] struct {
	v atomic.Value
}

// wrappedValue is used to wrap a value T in a concrete type,
// otherwise atomic.Value.Store may panic due to mismatching types in interfaces.
// This wrapping is not necessary for non-interface kinds of T,
// but there is no harm in wrapping anyways.
// See https://cs.opensource.google/go/go/+/refs/tags/go1.22.2:src/sync/atomic/value.go;l=78
type wrappedValue[T any] struct{ v T }

// Load returns the value set by the most recent Store.
// It returns the zero value for T if the value is empty.
func (v *AtomicValue[T]) Load() T {
	x, _ := v.LoadOk()
	return x
}

// LoadOk is like Load but returns a boolean indicating whether the value was
// loaded.
func (v *AtomicValue[T]) LoadOk() (_ T, ok bool) {
	x := v.v.Load()
	if x != nil {
		return x.(wrappedValue[T]).v, true
	}
	var zero T
	return zero, false
}

// Store sets the value of the Value to x.
func (v *AtomicValue[T]) Store(x T) {
	v.v.Store(wrappedValue[T]{x})
}

// Swap stores new into Value and returns the previous value.
// It returns the zero value for T if the value is empty.
func (v *AtomicValue[T]) Swap(x T) (old T) {
	oldV := v.v.Swap(wrappedValue[T]{x})
	if oldV != nil {
		return oldV.(wrappedValue[T]).v
	}
	return old // zero value of T
}

// CompareAndSwap executes the compare-and-swap operation for the Value.
// It panics if T is not comparable.
func (v *AtomicValue[T]) CompareAndSwap(oldV, newV T) (swapped bool) {
	var zero T
	return v.v.CompareAndSwap(wrappedValue[T]{oldV}, wrappedValue[T]{newV}) ||
		// In the edge-case where [atomic.Value.Store] is uninitialized
		// and trying to compare with the zero value of T,
		// then compare-and-swap with the nil any value.
		(any(oldV) == any(zero) && v.v.CompareAndSwap(any(nil), wrappedValue[T]{newV}))
}

// MutexValue is a value protected by a mutex.
//
// AtomicValue, [MutexValue], [atomic.Pointer] are similar and
// overlap in their use cases.
//
//   - Use [atomic.Pointer] if the value being stored is a pointer and
//     you only ever need load and store operations.
//     An atomic pointer only occupies 1 word of memory.
//
//   - Use [MutexValue] if the value being stored is not a pointer or
//     you need the ability for a mutex to protect a set of operations
//     performed on the value.
//     A mutex-guarded value occupies 1 word of memory plus
//     the memory representation of T.
//
//   - AtomicValue is useful for non-pointer types that happen to
//     have the memory layout of a single pointer.
//     Examples include a map, channel, func, or a single field struct
//     that contains any prior types.
//     An atomic value occupies 2 words of memory.
//     Consequently, Storing of non-pointer types always allocates.
//
// Note that [AtomicValue] has the ability to report whether it was set
// while [MutexValue] lacks the ability to detect if the value was set
// and it happens to be the zero value of T. If such a use case is
// necessary, then you could consider wrapping T in [opt.Value].
type MutexValue[T any] struct {
	mu Mutex
	v  T
}

// WithLock calls f with a pointer to the value while holding the lock.
// The provided pointer must not leak beyond the scope of the call.
func (m *MutexValue[T]) WithLock(f func(p *T)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(&m.v)
}

// Load returns a shallow copy of the underlying value.
func (m *MutexValue[T]) Load() T {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.v
}

// Store stores a shallow copy of the provided value.
func (m *MutexValue[T]) Store(v T) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.v = v
}

// Swap stores new into m and returns the previous value.
func (m *MutexValue[T]) Swap(new T) (old T) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, m.v = m.v, new
	return old
}

// Semaphore is a counting semaphore. Use NewSemaphore to create one.
type Semaphore struct {
	c chan struct{}
}

// NewSemaphore returns a semaphore with resource count n.
func NewSemaphore(n int) Semaphore {
	return Semaphore{c: make(chan struct{}, n)}
}

// Len reports the number of in-flight acquisitions.
// It is incremented whenever the semaphore is acquired.
// It is decremented whenever the semaphore is released.
func (s Semaphore) Len() int {
	return len(s.c)
}

// Acquire blocks until a resource is acquired.
func (s Semaphore) Acquire() {
	s.c <- struct{}{}
}

// AcquireContext reports whether the resource was acquired before the ctx was done.
func (s Semaphore) AcquireContext(ctx context.Context) bool {
	select {
	case s.c <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// TryAcquire reports, without blocking, whether the resource was acquired.
func (s Semaphore) TryAcquire() bool {
	select {
	case s.c <- struct{}{}:
		return true
	default:
		return false
	}
}

// Release releases a resource.
func (s Semaphore) Release() {
	<-s.c
}
