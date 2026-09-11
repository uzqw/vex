//go:build !noasm && amd64

#include "textflag.h"

// prefetchT0 issues PREFETCHT0 for the first cache line of a vector. The
// neighbor loop issues one per edge ahead of the distance pass; the hardware
// prefetcher picks up the sequential stream from there.
TEXT ·prefetchT0(SB), NOSPLIT, $0-8
	MOVQ addr+0(FP), AX
	PREFETCHT0 (AX)
	RET
