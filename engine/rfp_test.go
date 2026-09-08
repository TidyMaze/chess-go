package engine

import "testing"

// The zero-window predicate must accept the windows the search builds.
func TestZeroWindowAcceptsTheSearchsOwnWindows(t *testing.T) {
	for _, a := range []float64{-3.25, -0.5, 0, 0.5, 0.75, 2.1234} {
		if !zeroWindow(a, a+1e-6) {
			t.Errorf("alpha %v: the search's own null window was not recognised", a)
		}
	}
	if zeroWindow(-1, 1) || zeroWindow(0, 0.01) {
		t.Error("a real window was called zero")
	}
}

// Reverse futility past depth 3: a static evaluation this far beyond the
// window at a zero-window node is not going to be dragged back inside by
// searching, so the node returns it. The margin grows with depth.
func TestReverseFutilityMarginGrowsWithDepth(t *testing.T) {
	prev := 0.0
	for d := 1; d <= 7; d++ {
		m := reverseFutilityMargin(d)
		if m <= prev {
			t.Errorf("margin at depth %d is %.2f, not above depth %d's %.2f", d, m, d-1, prev)
		}
		prev = m
	}
	if m := reverseFutilityMargin(7); m < 2.5 || m > 3.5 {
		t.Errorf("depth 7 margin %.2f, expected around three pawns", m)
	}
}

func TestReverseFutilityCutConditions(t *testing.T) {
	const alpha, beta = 0.5, 0.5 + 1e-6 // a zero window around half a pawn
	// Maximizing side, static eval far above beta: cut.
	if !reverseFutilityCuts(5, 5.0, alpha, beta, true, false) {
		t.Error("maximizing, eval 5.0 against beta 0.5 at depth 5: should cut")
	}
	// Not far enough: no cut.
	if reverseFutilityCuts(5, 1.0, alpha, beta, true, false) {
		t.Error("maximizing, eval 1.0 against beta 0.5 at depth 5: margin not cleared")
	}
	// Minimizing side mirrors it against alpha.
	if !reverseFutilityCuts(5, -5.0, alpha, beta, false, false) {
		t.Error("minimizing, eval -5.0 against alpha 0.5: should cut")
	}
	if reverseFutilityCuts(5, 0.0, alpha, beta, false, false) {
		t.Error("minimizing, eval 0.0 against alpha 0.5: margin not cleared")
	}
	// Never in check, never at depths the shallow rule already covers or
	// past the cap, never on a mate-bound window, never on a real window.
	if reverseFutilityCuts(5, 9.0, alpha, beta, true, true) {
		t.Error("cut while in check")
	}
	if reverseFutilityCuts(3, 9.0, alpha, beta, true, false) || reverseFutilityCuts(8, 9.0, alpha, beta, true, false) {
		t.Error("cut outside depth 4..7")
	}
	if reverseFutilityCuts(5, 9.0, -mateScore, beta, true, false) {
		t.Error("cut on a mate-bound window")
	}
	if reverseFutilityCuts(5, 9.0, -1.0, 1.0, true, false) {
		t.Error("cut on an open window")
	}
}
