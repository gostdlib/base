//go:build race

package parker

// casInt32 is the race-build CAS used inside gopark's unlockf. The body
// lives in cas_race_arm64.s / cas_race_amd64.s — implementing it in asm
// keeps the call out of sync/atomic, whose race-instrumented variant
// crashes when called from g0 (where unlockf runs).

//go:nosplit
//go:norace
func casInt32(addr *int32, old, new int32) bool
