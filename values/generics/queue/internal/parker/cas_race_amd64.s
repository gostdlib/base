//go:build race

#include "textflag.h"

// func casInt32(addr *int32, old, new int32) bool
TEXT ·casInt32(SB), NOSPLIT, $0-17
	MOVQ	addr+0(FP), BX
	MOVL	old+8(FP), AX
	MOVL	new+12(FP), CX
	LOCK
	CMPXCHGL	CX, 0(BX)
	SETEQ	ret+16(FP)
	RET
