//go:build !race

package parker

import "sync/atomic"

// casInt32 forwards to sync/atomic in non-race builds (inlinable, zero
// overhead). The race build provides an asm body in cas_race_*.s so the
// call from gopark's unlockf (which runs on g0) never enters
// sync/atomic's race-instrumented variant.
//
//go:nosplit
func casInt32(addr *int32, old, new int32) bool {
	return atomic.CompareAndSwapInt32(addr, old, new)
}
