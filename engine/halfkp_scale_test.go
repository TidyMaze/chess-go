package engine

import (
	"testing"

	"chess/game"
)

// The search consumes evaluations in pawns: 0 is equal, positive favours
// White, and a queen is worth about nine. A network trained against a
// win-probability target does not produce that. It produces a number in
// [0,1], so an equal position reads as +0.5 and, fatally, a lost
// position reads as a small positive because probabilities are never
// negative.
//
// Measured before the fix, with a network that explained 82% of held-out
// variance:
//
//	start position        net +0.504  (should be ~0)
//	White up a queen      net +0.955  (should be ~+9)
//	Black up a rook       net +0.245  (should be ~-5, and it is POSITIVE)
//
// That last line is the whole story of every negative network result in
// this project: the engine was told that being a rook down was slightly
// good. The network was not wrong, it was being read in the wrong units.
func TestHalfKPEvaluatesInPawnsNotProbability(t *testing.T) {
	n, err := LoadHalfKPNet("../halfkp_latest.json")
	if err != nil {
		t.Skip("no trained network available")
	}
	// A network saved under a different feature set cannot be read by the
	// current one, and Evaluate correctly returns zero for it. Skip
	// rather than fail: that is a stale file, not a broken evaluation.
	if n.H == 0 || len(n.W1) != HalfKPInputs*n.H {
		t.Skipf("network on disk has %d first-layer weights, current feature set needs %d",
			len(n.W1), HalfKPInputs*n.H)
	}
	load := func(fen string) *game.Game {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatalf("ParseFEN(%q): %v", fen, err)
		}
		return g
	}

	start := n.Evaluate(&load("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1").Board)
	if start < -1.0 || start > 1.0 {
		t.Errorf("start position evaluates to %+.3f, want roughly 0", start)
	}

	queenUp := n.Evaluate(&load("rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1").Board)
	if queenUp < 2.0 {
		t.Errorf("a queen ahead evaluates to %+.3f, want clearly positive", queenUp)
	}

	rookDown := n.Evaluate(&load("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/1NBQKBNR w KQkq - 0 1").Board)
	if rookDown > -1.0 {
		t.Errorf("a rook behind evaluates to %+.3f, want clearly negative", rookDown)
	}

	if !(rookDown < start && start < queenUp) {
		t.Errorf("evaluations are not ordered: rook down %+.3f, start %+.3f, queen up %+.3f",
			rookDown, start, queenUp)
	}
	t.Logf("rook down %+.3f  <  start %+.3f  <  queen up %+.3f", rookDown, start, queenUp)
}
