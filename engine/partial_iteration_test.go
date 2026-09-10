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
