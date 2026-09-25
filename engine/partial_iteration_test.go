package engine

import (
	"testing"

	"chess/game"
)

// The clock nearly always runs out inside an iteration (87% of the budget
// is used on average, with the deadline at 105%), and that iteration's
// work was thrown away: the previous iteration's move stood whatever the
// deeper search had already found. A move that completed one ply deeper
// and beat the standing choice on an exact score is the better move by the
// deeper search, and a child cut off mid-search must never be believed.
//
// The abort is by node count here, not by clock, so every run is exact
// and repeatable on any machine at any load.
func TestAbortedIterationKeepsACompletedBetterMove(t *testing.T) {
	defer func() { testAbortAtNodes, testRootScores = 0, nil }()
	const maxDepth = 5
	improved, checked := 0, 0
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		p := Strong(maxDepth)
		p.MainSEE = true
		bestAt := map[int]game.Move{}
		for d := 1; d <= maxDepth; d++ {
			q := p
			q.Depth = d
			SeedRandom(1)
			if m, ok := PlayerPick(q, g); ok {
				bestAt[d] = m
			}
		}
		if len(bestAt) < maxDepth {
			continue
		}
		SeedRandom(1)
		PlayerPick(p, g)
		total := LastSearchNodesValue()
		for pct := 5; pct < 100; pct += 5 {
			testAbortAtNodes = int64(total*pct/100) + 1
			testRootScores = map[game.Move]float64{}
			SeedRandom(1)
			m, ok := PlayerPick(p, g)
			scores := testRootScores
			testAbortAtNodes, testRootScores = 0, nil
			if !ok {
				t.Fatalf("%s: no move after an abort at %d%%", fen, pct)
			}
			completed := LastSearchDepth()
			standing := bestAt[completed]
			if m != standing {
				// A switch is allowed only when the cut-off iteration completed
				// both moves and rated the new one higher. Anything else is a
				// garbage score from an aborted child being believed.
				newScore, newDone := scores[m]
				oldScore, oldDone := scores[standing]
				if !newDone || !oldDone || newScore <= oldScore {
					t.Errorf("%s aborted at %d%%: played %v over %v at completed depth %d; deeper scores %v (done %v) vs %v (done %v)",
						fen, pct, m.UCI(), standing.UCI(), completed, newScore, newDone, oldScore, oldDone)
				}
				if newScore-oldScore < 0.01 {
					t.Errorf("switch allowed with noisy margin < 0.01: %v vs %v", newScore, oldScore)
				}
				t.Logf("switch at %d%%: %v (score %v) over %v (score %v)", pct, m.UCI(), newScore, standing.UCI(), oldScore)
				improved++
			}
			checked++
		}
	}
	t.Logf("%d abort points, %d kept the deeper iteration's move", checked, improved)
	if improved == 0 {
		t.Error("no abort point ever kept a deeper move: the aborted iteration's work is still thrown away")
	}
}

// A move taken from a cut-off iteration has to be visible from outside, so a
// study of short-clock blunders can tell them from moves of completed depths.
func TestTheSearchSaysWhenItsMoveCameFromACutOffIteration(t *testing.T) {
	defer func() { testAbortAtNodes = 0 }()
	g, err := game.ParseFEN(correctnessPositions[0])
	if err != nil {
		t.Fatal(err)
	}
	p := Strong(5)
	p.MainSEE = true
	SeedRandom(1)
	PlayerPick(p, g)
	if LastMoveFromCutOffIteration() {
		t.Error("a search that completed every iteration reports a cut-off move")
	}
	total := LastSearchNodesValue()
	testAbortAtNodes = int64(total*90/100) + 1 // the abort point that switches, see below
	SeedRandom(1)
	PlayerPick(p, g)
	if !LastMoveFromCutOffIteration() {
		t.Error("the move switched to inside a cut-off iteration is not reported as such")
	}
}

func TestAbortedIterationRejectsFailLowStanding(t *testing.T) {
	defer func() { testAbortAtNodes, testRootScores, testForceStandingFailLow = 0, nil, false }()
	fen := correctnessPositions[0]
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}
	p := Strong(5)
	p.MainSEE = true

	// Find the baseline standing move at completed depth 4
	p4 := p
	p4.Depth = 4
	SeedRandom(1)
	standing, ok := PlayerPick(p4, g)
	if !ok {
		t.Fatal("failed to get baseline move")
	}

	SeedRandom(1)
	PlayerPick(p, g)
	total := LastSearchNodesValue()

	// At 90%, it normally switches to d2d4 because d2d4 beats e2e4.
	testAbortAtNodes = int64(total*90/100) + 1
	testForceStandingFailLow = false
	SeedRandom(1)
	switchedMove, ok := PlayerPick(p, g)
	if !ok || switchedMove == standing {
		t.Fatalf("expected switch at 90%% nodes without fail-low force, got %v vs standing %v", switchedMove, standing)
	}

	// Now force standing fail-low: the aborted iteration must NOT switch!
	testForceStandingFailLow = true
	SeedRandom(1)
	keptMove, ok := PlayerPick(p, g)
	if !ok {
		t.Fatal("failed to get move with fail-low forced")
	}
	if keptMove != standing {
		t.Errorf("aborted iteration switched to %v despite standing failing low; must keep %v", keptMove, standing)
	}
}
