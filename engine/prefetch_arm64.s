//go:build arm64 && !purego

#include "textflag.h"

// func prefetchLine(p unsafe.Pointer)
TEXT ·prefetchLine(SB), NOSPLIT|NOFRAME, $0-8
	MOVD	p+0(FP), R0
	PRFM	(R0), PLDL1KEEP
	RET
