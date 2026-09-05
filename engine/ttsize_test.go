package engine

import (
	"testing"
	"unsafe"
)

// The whole point of the packing was the size, so the size is asserted.
func TestTTEntryStaysSmall(t *testing.T) {
	if got := unsafe.Sizeof(ttEntry{}); got != 24 {
		t.Errorf("ttEntry is %d bytes, want 24 (a 2^20 table is %d MB)", got, got*(1<<20)/(1<<20))
	}
}
