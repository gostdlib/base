//go:build race

#include "textflag.h"

// func casInt32(addr *int32, old, new int32) bool
//
// On success (old == *addr), stores new to *addr and returns true.
// On failure, returns false. Uses LL/SC; portable across all arm64
// (does not require LSE).
TEXT ·casInt32(SB), NOSPLIT, $0-17
	MOVD	addr+0(FP), R0
	MOVW	old+8(FP), R1
	MOVW	new+12(FP), R2
loop:
	LDAXRW	(R0), R3
	CMPW	R1, R3
	BNE	done
	STLXRW	R2, (R0), R3
	CBNZ	R3, loop
done:
	CSET	EQ, R0
	MOVB	R0, ret+16(FP)
	RET
