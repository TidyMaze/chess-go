package engine

import "testing"

// Null move with a gate and an adaptive reduction. The gate: only try a
// null move when the static evaluation already stands at or beyond the
// bound, since a null search from a position below beta almost never
// cuts and its cost is pure waste. The reduction grows with depth and
// with how far the static evaluation exceeds the bound.
func TestNullMoveGate(t *testing.T) {
	if !nullMoveAllowed(1.0, 0.5, true) || nullMoveAllowed(0.0, 0.5, true) {
		t.Error("maximizing: allowed iff static eval >= beta")
	}
	if !nullMoveAllowed(0.0, 0.5, false) || nullMoveAllowed(1.0, 0.5, false) {
		t.Error("minimizing: allowed iff static eval <= alpha")
	}
}

func TestNullMoveReductionGrows(t *testing.T) {
	if nullMoveReduction(3, 0) != 3 {
		t.Errorf("depth 3 at the bound: %d, want the historical 3", nullMoveReduction(3, 0))
	}
	if nullMoveReduction(12, 0) <= nullMoveReduction(3, 0) {
		t.Error("reduction must grow with depth")
	}
	if nullMoveReduction(6, 4.0) <= nullMoveReduction(6, 0) {
		t.Error("reduction must grow with the margin over the bound")
	}
	// Capped at the depth: the null search ends in quiescence at worst.
	for d := 3; d <= 20; d++ {
		if r := nullMoveReduction(d, 9.0); r > d {
			t.Errorf("depth %d: reduction %d exceeds the depth", d, r)
		}
	}
	if nullMoveReduction(3, 9.0) != 3 {
		t.Errorf("depth 3 with a huge margin: %d, want the cap 3", nullMoveReduction(3, 9.0))
	}
}
