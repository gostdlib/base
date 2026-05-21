package parker

import (
	"sync/atomic"
	"unsafe"
	_ "unsafe" // for go:linkname

	"github.com/gostdlib/base/concurrency/sync"
)

// runtime.gopark / runtime.goready are not in the public API; we reach them
// by linkname. Both are on Go's checklinkname exception list (golang/go#67401)
// so this builds with default flags on go1.21+. If a future Go version drops
// them or changes the signature, this file is the only thing to update.

type waitReason uint8       // runtime.waitReason (0 = "" — fine for our use)
type traceBlockReason uint8 // runtime.traceBlockReason

// The runtime types unlockf's first arg and goready's gp as *g. We mirror
// it as uintptr — same ABI, but no GC write barriers and no chance of the
// compiler treating it as a managed pointer. This matches the convention
// used by gvisor, sagernet, and other long-standing linkname callers.

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

// parker holds one waiter slot. The state field is the synchronization
// point: the write to g happens-before the CAS to parkParked, so any
// reader that observes parkParked via Swap is guaranteed to see g.
type parker struct {
	state atomic.Int32
	g     uintptr // captured inside unlockf; 0 until then
}

// waiter is a one-shot broadcast point. gen encodes the cycle in one
// atomic: even = current cycle waiting, odd = current cycle fired.
type waiter struct {
	mu      sync.Mutex
	parkers []*parker
	gen     atomic.Uint64
}

func newWaiter() *waiter {
	return &waiter{}
}

func (w *waiter) wait() {
	startGen := w.gen.Load()
	if startGen&1 == 1 {
		return // already fired in this cycle
	}

	p := &parker{}

	w.mu.Lock()
	if w.gen.Load() != startGen {
		w.mu.Unlock()
		return
	}
	w.parkers = append(w.parkers, p)
	w.mu.Unlock()

	// Pass p through the lock argument rather than capturing it in a
	// closure. The runtime invokes unlockf on the M's g0 stack, where the
	// race detector's instrumentation (which closures get by default)
	// misreads g0 as a user goroutine and corrupts state. A top-level
	// //go:norace function avoids that instrumentation entirely.
	gopark(parkerUnlockf, unsafe.Pointer(p), 0, 0, 1)
}

// parkerUnlockf is gopark's unlockf for waiter.wait. It runs on g0 after
// the parking G has been moved to waiting state. gp is the parking G's
// *g; the lock arg is the *parker (passed via gopark's lock parameter).
//
//go:norace
//go:nosplit
func parkerUnlockf(gp uintptr, lock unsafe.Pointer) bool {
	p := (*parker)(lock)
	p.g = gp
	return p.state.CompareAndSwap(parkInit, parkParked)
}

func (w *waiter) broadcast() {
	w.mu.Lock()
	cur := w.gen.Load()
	if cur&1 == 1 {
		w.mu.Unlock()
		return // idempotent within a cycle
	}
	w.gen.Store(cur + 1) // even → odd
	parkers := w.parkers
	w.parkers = nil
	w.mu.Unlock()

	for _, p := range parkers {
		// State == Parked means unlockf ran, *g was captured, and the G
		// is in waiting state. Any other prior value means the G is still
		// on its way to gopark and will see Woken in unlockf → abort park.
		if p.state.Swap(parkWoken) == parkParked {
			goready(p.g, 1)
		}
	}
}

func (w *waiter) reset() {
	w.mu.Lock()
	cur := w.gen.Load()
	var next uint64
	if cur&1 == 1 {
		next = cur + 1 // odd → next even
	} else {
		next = cur + 2 // even → skip past an unused fired slot
	}
	w.gen.Store(next)
	stragglers := w.parkers
	w.parkers = nil
	w.mu.Unlock()

	for _, p := range stragglers {
		if p.state.Swap(parkWoken) == parkParked {
			goready(p.g, 1)
		}
	}
}
