package engine

import (
	"testing"

	"chess/board"
)

// 64 king slots are the raw king square, nothing mirrored or grouped, so a
// pool generated with them can be re-bucketed into any coarser scheme later.
func TestSixtyFourKingSlotsAreTheRawSquare(t *testing.T) {
	g1, b1, g5 := board.Sq{File: 6, Rank: 0}, board.Sq{File: 1, Rank: 0}, board.Sq{File: 6, Rank: 4}
	if got := kingSlot(g1, 64); got != 6 {
		t.Errorf("g1 in 64 slots is %d, want 6", got)
	}
	if kingSlot(g1, 64) == kingSlot(b1, 64) {
		t.Error("g1 and b1 must be different slots when nothing is mirrored")
	}
	if got := kingSlot(g5, 64); got != 38 {
		t.Errorf("g5 in 64 slots is %d, want 38", got)
	}
	if kingSlot(g1, 8) != kingSlot(b1, 8) {
		t.Error("the 8-bucket scheme must keep mirroring files, older networks depend on it")
	}
	if HalfKPInputsFor(64) != 40960 {
		t.Errorf("64 slots give %d inputs, want 40960", HalfKPInputsFor(64))
	}
}
