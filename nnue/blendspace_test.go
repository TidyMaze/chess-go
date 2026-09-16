package main

import (
	"math"
	"testing"
)

// Blending a game outcome into a position's target only makes sense in
// probability space. resultPawns caps a win at +4 pawns, so blending in
// pawns drags a clearly winning position *down* toward +4: a position the
// search calls +8 in a game that was won comes out at +6.8, and the label
// now says the position is worse than it is because the side to move
// happened to win. In probability space a win can only push the target up.
func TestPawnBlendDragsWinningPositionsDown(t *testing.T) {
	const k, lambda = 0.30, 0.7
	const won = 1.0
	const winning = 8.0

	pawns := blendedTarget(winning, won, lambda)
	prob := blendedTargetProb(winning, won, lambda, k)
	t.Logf("search says %+.1f and the game was won: pawn blend %+.2f, probability blend %+.2f", winning, pawns, prob)
	if pawns >= winning {
		t.Errorf("pawn blend gave %+.2f; it is supposed to be the one that pulls a win down to +4", pawns)
	}
	if prob < winning {
		t.Errorf("probability blend gave %+.2f, below the search score %+.1f: a win must never lower the target", prob, winning)
	}

	// And a loss must never raise it.
	if got := blendedTargetProb(-winning, 0, lambda, k); got > -winning {
		t.Errorf("a lost game raised a losing position from %+.1f to %+.2f", -winning, got)
	}
}

// At lambda 1 the outcome has no weight, so both spaces must return the
// search score itself. Any disagreement here is a scale bug.
func TestBothSpacesAgreeWhenTheOutcomeHasNoWeight(t *testing.T) {
	for _, score := range []float64{-2.5, 0, 0.75, 5} {
		for _, result := range []float64{0, 0.5, 1} {
			p := blendedTargetProb(score, result, 1, 0.30)
			if math.Abs(p-score) > 1e-6 {
				t.Errorf("lambda 1, score %+.2f, result %.1f: probability blend gave %+.4f", score, result, p)
			}
		}
	}
}

// A decided game with no search opinion is still bounded: the network
// cannot represent a mate, and an infinite target would poison the batch.
func TestProbabilityBlendStaysInRange(t *testing.T) {
	for _, result := range []float64{0, 1} {
		got := blendedTargetProb(0, result, 0, 0.30)
		if math.IsInf(got, 0) || math.IsNaN(got) || math.Abs(got) > 12 {
			t.Errorf("pure outcome %.0f gave %v, outside the representable range", result, got)
		}
	}
}
