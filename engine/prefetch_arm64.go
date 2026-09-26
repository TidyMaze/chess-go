//go:build arm64 && !purego

package engine

import "unsafe"

// prefetchLine asks the core to start loading p's cache line (PRFM
// PLDL1KEEP). A prefetch is a hint: it never faults and reads nothing, so
// the search cannot tell it ran. Go has no public prefetch, hence the
// assembly.
//
//go:noescape
func prefetchLine(p unsafe.Pointer)
