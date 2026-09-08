package board

import "testing"

func TestRemoveOnAnEmptySquareIsANoOp(t *testing.T) {
	b := Initial()
	empty := Sq{File: 4, Rank: 4}
	before := b.PieceCount()
	b.Remove(empty)
	if b.PieceCount() != before {
		t.Errorf("removing from an empty square changed the piece count %d -> %d", before, b.PieceCount())
	}
}
