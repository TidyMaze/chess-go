package engine

import (
	"testing"

	"chess/game"
)

// The reduction for a late quiet move, as a pure function of what the
// search knows about it. Logarithmic in depth and move number, lighter
// at PV nodes, one ply less for a killer, none for a promotion, and
// never so deep that nothing but quiescence remains.
func TestLMRReductionShape(t *testing.T) {
	// Grows with both depth and move number.
	if lmrReduction(3, 3, false, false, false) > lmrReduction(8, 3, false, false, false) {
		t.Error("reduction must not shrink with depth")
	}
	if lmrReduction(6, 3, false, false, false) > lmrReduction(6, 30, false, false, false) {
		t.Error("reduction must not shrink with move number")
	}
	// At least one ply once it applies, never into quiescence.
	for d := 3; d <= 12; d++ {
		for m := 3; m < 60; m += 7 {
			r := lmrReduction(d, m, false, false, false)
			if r < 1 || r > d-2 {
				t.Errorf("depth %d move %d: reduction %d out of [1, %d]", d, m, r, d-2)
			}
		}
	}
	// A non-killer whose table value rounds to zero is still reduced a ply.
	if lmrReduction(3, 1, false, false, false) != 1 {
		t.Errorf("depth 3 move 1: %d, want 1", lmrReduction(3, 1, false, false, false))
	}
	// Below depth 3 nothing is reduced.
	if lmrReduction(2, 30, false, false, false) != 0 {
		t.Error("reduced at depth 2")
	}
}

func TestLMRReductionExemptionsAndDiscounts(t *testing.T) {
	base := lmrReduction(10, 20, false, false, false)
	if base < 2 {
		t.Fatalf("control reduction too small to test discounts: %d", base)
	}
	if lmrReduction(10, 20, false, false, true) != 0 {
		t.Error("a promotion was reduced")
	}
	if k := lmrReduction(10, 20, false, true, false); k != base-1 {
		t.Errorf("killer: %d, want one less than %d", k, base)
	}
	if pv := lmrReduction(10, 20, true, false, false); pv >= base || pv < 1 {
		t.Errorf("PV node: %d, want lighter than %d and at least 1", pv, base)
	}
	// A killer at the shallowest reducible depth: the discount cannot take
	// the reduction below one ply while it applies... it takes it to zero,
	// which means "search in full", and that is the intent.
	if lmrReduction(3, 3, false, true, false) != 0 {
		t.Error("killer at depth 3 should not be reduced")
	}
}

// Wired in, the logarithmic table must remove nodes against the flat
// one-ply reduction.
func TestLogarithmicLMRCutsNodes(t *testing.T) {
	var flat, log int
	for _, fen := range correctnessPositions[:6] {
		g, _ := game.ParseFEN(fen)
		a, b := Strong(6), Strong(6)
		b.ScaledLMR = true
		ResetNodes()
		PlayerScoreWith(a, g, nil)
		flat += TotalNodes()
		ResetNodes()
		PlayerScoreWith(b, g, nil)
		log += TotalNodes()
	}
	t.Logf("nodes: %d flat LMR, %d logarithmic (%.2fx)", flat, log, float64(flat)/float64(log))
	if log >= flat {
		t.Errorf("logarithmic reductions removed no nodes: %d against %d", log, flat)
	}
}

// TestLMRTwoStepCutsNodes verifies that confirming reduced fail-highs with a
// zero-window full-depth search reduces nodes visited across test positions.
func TestLMRTwoStepCutsNodes(t *testing.T) {
	var oneStep, twoStep int
	for _, fen := range correctnessPositions[:6] {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		a, b := Strong(6), Strong(6)
		a.LMRTwoStep = false
		b.LMRTwoStep = true
		// Seeded: the tie-break steers the next iteration's ordering, and
		// unseeded the 1 to 3% margin moved with however many random draws
		// the tests before this one had made.
		SeedRandom(1)
		ResetNodes()
		m1, ok1 := PlayerPick(a, g)
		oneStep += TotalNodes()
		SeedRandom(1)
		ResetNodes()
		m2, ok2 := PlayerPick(b, g)
		twoStep += TotalNodes()

		if !ok1 || !ok2 {
			t.Fatalf("PlayerPick failed on %s: ok1=%v, ok2=%v", fen, ok1, ok2)
		}
		if m1.From == m1.To || m2.From == m2.To {
			t.Errorf("null move on %s: m1=%v, m2=%v", fen, m1, m2)
		}
	}
	t.Logf("nodes: %d single-step, %d two-step (%.2fx)", oneStep, twoStep, float64(oneStep)/float64(twoStep))
	if twoStep >= oneStep {
		t.Errorf("two-step LMR re-search did not reduce nodes: single=%d, two=%d", oneStep, twoStep)
	}
}
