//go:build !arm64 || purego

package engine

import "unsafe"

// prefetchLine is a no-op off arm64: the probe simply waits for its line.
func prefetchLine(p unsafe.Pointer) {}
