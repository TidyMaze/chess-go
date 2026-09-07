package engine

import (
	"math"
	"testing"

	"chess/game"
)

// Is the aspiration window free in the configuration that actually plays?
//
// It searches a narrow window around the previous iteration's score and
// re-searches on a fail, so it is meant to be a pure speed optimisation:
// same answer, fewer nodes. It was measured at -86 Elo and switched off,
// but that measurement ran through a transposition table that keyed every
// node on the root's side to move, and the window is exactly what decides
// whether a poisoned entry is believed. So the question is open again.
//
// It has to be asked through PlayerPick. Aspiration lives in the iterative
// deepening loop, because it needs the previous iteration's score to build
// a window around; PlayerScoreWith searches one fixed depth and never
// touches it. A first version of this test compared PlayerScoreWith with
// the window on and off, got byte-identical node counts, and was measuring
// nothing at all.
func TestAspirationIsNeutralAtPlayingDepth(t *testing.T) {
	if testing.Short() {
		t.Skip("searches to depth 6")
	}
	var nodesOff, nodesOn int64
	moveChanges := 0
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		for depth := 4; depth <= 6; depth++ {
			off, on := Strong(depth), Strong(depth)
			off.Aspiration, on.Aspiration = false, true

			ResetNodes()
			mOff, ok1 := PlayerPick(off, g)
			nodesOff += int64(TotalNodes())
			ResetNodes()
			mOn, ok2 := PlayerPick(on, g)
			nodesOn += int64(TotalNodes())
			if !ok1 || !ok2 {
				continue
			}
			if mOff != mOn {
				moveChanges++
			}
		}
	}
	t.Logf("nodes: %d without aspiration, %d with, %.2fx faster",
		nodesOff, nodesOn, float64(nodesOff)/float64(nodesOn))
	t.Logf("move differed in %d of %d searches", moveChanges, len(correctnessPositions)*3)

	// Exact and cheaper is not a trade-off, it is free. If it ever stops
	// being cheaper there is no reason to keep it.
	if nodesOn >= nodesOff {
		t.Errorf("aspiration searched %d nodes against %d without it: the only reason "+
			"to keep it is speed, and there is none here", nodesOn, nodesOff)
	}
}

// Does the window cost move quality? Measured against the reference search
// rather than against the no-window engine, so a difference is a real
// mistake and not just a different tie broken differently.
func TestAspirationDoesNotPickWorseMoves(t *testing.T) {
	if testing.Short() {
		t.Skip("scores every root move exactly")
	}
	const depth = 4
	worstOn, worstOff, worstFEN := 0.0, 0.0, ""
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		values := exactMoveValues(t, g, depth)
		best := math.Inf(-1)
		for _, v := range values {
			if v > best {
				best = v
			}
		}
		off, on := Strong(depth), Strong(depth)
		off.Aspiration, on.Aspiration = false, true
		mOff, ok1 := PlayerPick(off, g)
		mOn, ok2 := PlayerPick(on, g)
		if !ok1 || !ok2 {
			continue
		}
		if l := best - values[mOff]; l > worstOff {
			worstOff = l
		}
		if l := best - values[mOn]; l > worstOn {
			worstOn, worstFEN = l, fen
		}
	}
	t.Logf("worst move loss against the reference: %.3f pawns with the window, "+
		"%.3f without (%s)", worstOn, worstOff, worstFEN)
	// Both are held to the same bound rather than to each other.
	//
	// Comparing them directly is not a measurement: PlayerPick breaks ties
	// at random, and on this set two moves tie at the engine's depth while
	// differing by 0.83 pawns at the reference's. Successive runs handed
	// that 0.83 to whichever configuration got unlucky, so the first
	// version of this test "proved" aspiration better and then, unchanged,
	// proved it worse. Whether the window costs strength is a question for
	// a match: 400 games say -5 +/- 34 at depth 4 and +3 +/- 34 at depth 5.
	const bound = 2.5
	if worstOn > bound || worstOff > bound {
		t.Errorf("move loss against the reference exceeded %.1f pawns: %.3f with the "+
			"window, %.3f without (%s)", bound, worstOn, worstOff, worstFEN)
	}
}
