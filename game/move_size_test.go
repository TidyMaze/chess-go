package game

import (
	"testing"
	"unsafe"
)

// The search copies a move into every move list, killer slot and history
// probe, so a wider move slows every node.
func TestMoveFitsIn32Bytes(t *testing.T) {
	if got := unsafe.Sizeof(Move{}); got > 32 {
		t.Fatalf("game.Move is %d bytes, want at most 32", got)
	}
}
