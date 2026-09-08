package engine

import (
	"math"
	"testing"

	"chess/game"
)

// Deeper must never be worse where "worse" is unambiguous: a forced mate.
//
// Before the transposition table fix, depth 6 lost to depth 5 by 185 Elo,
// which no correct search can do. The score-level form of that failure is
// a deeper search that finds a mate a shallower one found, then loses it
// again, or finds it slower. Mate scores are mateScore plus the depth left
// when the mate lands, so at a fixed mate the score must grow with root
// depth and its sign must never change once a mate is seen.
//
// Both colours, because a sign bug shows on only one of them.
func TestDeeperSearchNeverLosesAMate(t *testing.T) {
	positions := []string{
		"6k1/5ppp/8/8/8/8/5PPP/R5K1 w - - 0 1", // Ra8#
		"r5k1/5ppp/8/8/8/8/5PPP/6K1 b - - 0 1", // Ra1#
		"k7/8/1K6/8/8/8/8/7R w - - 0 1",        // Rh8#
		"7k/8/5K2/8/8/8/8/Q7 w - - 0 1",        // Qg7#
		"7r/8/8/8/8/1k6/8/K7 b - - 0 1",        // Rh1#, the mirror of Rh8# above
	}
	const mateBound = mateScore - maxSearchPly
	for _, fen := range positions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		firstMate, prev := 0, 0.0
		for depth := 1; depth <= 6; depth++ {
			s, ok := PlayerScoreWith(Strong(depth), g, nil)
			if !ok {
				t.Fatalf("%s: no legal moves", fen)
			}
			isMate := math.Abs(s) > mateBound
			if firstMate == 0 {
				if isMate {
					firstMate, prev = depth, s
				}
				continue
			}
			if !isMate {
				t.Errorf("%s\n  depth %d found a mate (%.0f) and depth %d lost it (%.3f)",
					fen, firstMate, prev, depth, s)
				continue
			}
			if math.Signbit(s) != math.Signbit(prev) {
				t.Errorf("%s\n  mate changed sign between depth %d (%.0f) and %d (%.0f)",
					fen, depth-1, prev, depth, s)
			}
			if math.Abs(s) < math.Abs(prev) {
				t.Errorf("%s\n  mate got slower with depth: %.0f at depth %d, %.0f at depth %d",
					fen, prev, depth-1, s, depth)
			}
			prev = s
		}
		if firstMate == 0 {
			t.Errorf("%s: no mate found within depth 6, the position is wrong", fen)
		}
	}
}
