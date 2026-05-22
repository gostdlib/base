// Package parker provides a low-overhead broadcast wake primitive built on
// the runtime's gopark/goready. It is intended as an internal building
// block for higher-level synchronization (e.g. a condition variable that
// avoids allocating a channel per broadcast).
//
// A Waiter is a single broadcast point. A goroutine reserves a slot via
// Register, then blocks on Parker.Park. Another goroutine releases every
// registered parker with Waiter.Broadcast, or a single one with
// Parker.Wake. After Broadcast the waiter's internal list is drained, so
// subsequent Register/Park cycles block again until the next Broadcast.
package parker

import (
	"sync/atomic"
	"unsafe"
	_ "unsafe" // for go:linkname

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/context"
)

// runtime.gopark / runtime.goready are not in the public API; we reach them
// by linkname. Both are on Go's checklinkname exception list (golang/go#67401)
// so this builds with default flags on go1.21+. If a future Go version drops
// them or changes the signature, this file is the only thing to update.

type waitReason uint8       // runtime.waitReason
type traceBlockReason uint8 // runtime.traceBlockReason

// The runtime types unlockf's first arg and goready's gp as *g. We mirror
// it as uintptr — same ABI, but no GC write barriers and no chance of the
// compiler treating it as a managed pointer.

//go:linkname gopark runtime.gopark
func gopark(
	unlockf func(uintptr, unsafe.Pointer) bool,
	lock unsafe.Pointer,
	reason waitReason,
	traceReason traceBlockReason,
	traceskip int,
)

//go:linkname goready runtime.goready
func goready(gp uintptr, traceskip int)

const (
	parkInit int32 = iota
	parkParked
	parkWoken
)

// Parker is one waiting slot returned by Waiter.Register. Exactly one of
// Park or Wake should be called per Parker; both are safe to interleave
// with a concurrent Broadcast.
//
// state is the synchronization point. The write to g (inside unlockf)
// happens-before the CAS to parkParked; any reader observing parkParked
// via SwapInt32 is guaranteed to see g.
//
// state is a plain int32 (not atomic.Int32) so we can pass its address to
// the asm-backed casInt32 used inside unlockf — see cas_race_*.s.
type Parker struct {
	state    int32   // parkInit | parkParked | parkWoken
	g        uintptr // captured inside unlockf; 0 until then
	waiter   *Waiter // set in Register, cleared in Release
	wakeFunc func()  // pre-bound p.Wake; lives for the Parker's pool tenure
}

// Waiter is a broadcast wake point. The zero value is not usable; call New.
type Waiter struct {
	mu      sync.Mutex
	parkers []*Parker
}

// New returns a fresh Waiter ready for Register/Park/Broadcast.
func New() *Waiter {
	return &Waiter{}
}

// parkerPool recycles *Parker across Register/Release cycles. Put invokes
// Parker.Reset (Resetter contract) which clears state and g via atomic
// stores so the race detector sees a happens-before edge between the
// previous owner's Broadcast/Wake and the next owner's Park.
// WithBuffer keeps recycled Parkers in a fixed-size channel so they survive
// across GC cycles — sync.Pool alone gets cleared on every GC, which for
// signal/wait churn means the pool is almost always empty when Register
// runs. A small buffer is enough to retain the working set.
var parkerPool = sync.NewPool(
	context.Background(),
	"queue/internal/parker.Parker",
	func() *Parker {
		p := &Parker{}
		// Pre-bind the Wake method value once, when the Parker is born.
		// Callers that need a func() to register with context.AfterFunc
		// (or similar) use WakeFunc() and avoid the per-call bound-method
		// allocation. Safe to reuse: bound method value remains valid as
		// long as the receiver pointer stays live.
		p.wakeFunc = p.Wake
		return p
	},
	sync.WithBuffer(64),
)

// Reset implements sync.Resetter. It runs automatically when a Parker is
// returned to the pool. Atomic stores establish a release edge on state
// and g so the next Get acquires a happens-before chain back to any
// concurrent Broadcast/Wake that touched this instance.
func (p *Parker) Reset() {
	atomic.StoreInt32(&p.state, parkInit)
	atomic.StoreUintptr(&p.g, 0)
	p.waiter = nil
}

// Register reserves a parking slot for the calling goroutine and returns
// its handle. The caller must follow with exactly one Park (to block) or
// Wake (to release without blocking) on the returned Parker. Broadcast
// resolves any registered parker that has neither parked nor been woken
// without leaking the slot.
//
// The returned Parker is drawn from a sync.Pool. Call Release after Park
// returns to recycle it; failing to call Release lets the Parker be
// garbage-collected normally, so Release is an optimization, not a
// correctness requirement.
func (w *Waiter) Register() *Parker {
	p := parkerPool.Get(context.Background())
	p.waiter = w
	w.mu.Lock()
	w.parkers = append(w.parkers, p)
	w.mu.Unlock()
	return p
}

// Detach removes p from its Waiter's parkers list without resetting any
// field and without returning p to the pool. Use this when a ctx-watcher
// callback (e.g. one registered with context.AfterFunc) may still be
// executing p.Wake concurrently: Wake touches p.state and p.g but not
// p.waiter, so list removal is race-free, while pool recycling would not
// be (a recycled Parker handed to a new caller would see Wake's stale
// state mutation). After Detach the Parker is unreachable from the
// Waiter and becomes garbage as soon as the watcher releases it.
func (p *Parker) Detach() {
	w := p.waiter
	if w == nil {
		return
	}
	w.mu.Lock()
	for i, q := range w.parkers {
		if q == p {
			w.parkers = append(w.parkers[:i], w.parkers[i+1:]...)
			break
		}
	}
	w.mu.Unlock()
	p.waiter = nil
}

// Release returns p to the internal pool for reuse. The caller must
// guarantee no other goroutine holds a reference to p — in particular,
// any ctx-watcher that might call p.Wake must have completed first. If in
// doubt, call Detach instead (or do nothing and let the GC reclaim p).
//
// Release removes p from its Waiter's list. If Broadcast already drained
// p (the common case for non-ctx-fire waits), the removal is a no-op.
func (p *Parker) Release() {
	w := p.waiter
	if w == nil {
		return // already released or never registered
	}
	w.mu.Lock()
	for i, q := range w.parkers {
		if q == p {
			w.parkers = append(w.parkers[:i], w.parkers[i+1:]...)
			break
		}
	}
	w.mu.Unlock()
	// Put invokes p.Reset (Resetter) which atomic-stores parkInit and 0;
	// that's what establishes happens-before with any concurrent Broadcast
	// that touched state via SwapInt32 before goready returned.
	parkerPool.Put(context.Background(), p)
}

// Park blocks the calling goroutine until Broadcast or Wake fires for this
// parker. Must be called by the same goroutine that received p from
// Register.
func (p *Parker) Park() {
	gopark(parkerUnlockf, unsafe.Pointer(p), 0, 0, 1)
}

// Wake releases p. If p is currently parked, the parked goroutine is
// scheduled to run. If p has been registered but has not yet parked, the
// pending Park aborts and returns immediately. Safe to call from any
// goroutine; idempotent.
//
// Wake does not remove p from its Waiter's list — a later Broadcast sees
// p in the parkWoken state and skips it. For signal-style workloads where
// Broadcast fires regularly, the list drains naturally.
func (p *Parker) Wake() {
	if atomic.SwapInt32(&p.state, parkWoken) == parkParked {
		// atomic.LoadUintptr on p.g (paired with atomic.StoreUintptr in
		// Reset) gives the race detector a total order on p.g and avoids
		// a false positive against the pool-recycle write.
		goready(atomic.LoadUintptr(&p.g), 1)
	}
}

// WakeFunc returns a func() that, when invoked, calls p.Wake. The returned
// function is pre-bound at pool allocation time, so callers passing it to
// context.AfterFunc or similar avoid the per-call bound-method-value
// allocation. Safe to call multiple times; same func every time. Becomes
// invalid after Release.
func (p *Parker) WakeFunc() func() { return p.wakeFunc }

// Broadcast wakes every parker currently registered on w and clears the
// list. After Broadcast returns, parkers that were already parked will be
// scheduled; parkers between Register and Park will abort their park and
// return from Park immediately.
func (w *Waiter) Broadcast() {
	w.mu.Lock()
	parkers := w.parkers
	w.parkers = nil
	w.mu.Unlock()

	for _, p := range parkers {
		// State == Parked means unlockf ran, *g was captured, and the G
		// is in waiting state. Any other prior value means either the G
		// is still on its way to gopark (and will see Woken in unlockf →
		// abort park), or Wake already handled it.
		if atomic.SwapInt32(&p.state, parkWoken) == parkParked {
			// atomic.LoadUintptr on p.g (paired with atomic.StoreUintptr
			// in Reset) gives the race detector a total order on p.g and
			// avoids a false positive against the pool-recycle write.
			goready(atomic.LoadUintptr(&p.g), 1)
		}
	}
}

// parkerUnlockf is gopark's unlockf for Parker.Park. It runs on g0 after
// the parking G has been moved to waiting state. gp is the parking G's
// *g; the lock arg is the *Parker (passed via gopark's lock parameter).
//
//go:norace
//go:nosplit
func parkerUnlockf(gp uintptr, lock unsafe.Pointer) bool {
	p := (*Parker)(lock)
	p.g = gp
	return casInt32(&p.state, parkInit, parkParked)
}
