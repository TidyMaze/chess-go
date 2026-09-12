package engine

import (
	"math"
	"testing"

	"chess/game"
)

// The search must return the same score for the same position, whatever
// ran before it.
//
// It did not. searchCtx comes from a sync.Pool and nothing cleared it, so
// each search inherited the previous one's killers, history, repetition
// path, and abort deadline. The repetition path is the one that changed
// answers: isRepetition scans path[0..ply], PlayerScoreWith never set
// path[0], and in self-play the stale root key of an earlier search
// matches a real position often enough to score it as a draw.
//
// Measured before the fix, comparing two configurations that differ only
// in a flag the code path never reads: 18 of 74 positions scored
// differently at depth 4, 24 at depth 5, and 40 at depth 6, the worst by
// 1.9 pawns. Afterwards, zero at every depth.
//
// This matters beyond play. PlayerScoreWith is what labels training data,
// so a corrupted score is a corrupted label.
func TestSearchScoreIsDeterministic(t *testing.T) {
	var positions []*game.Game
	g := game.New()
	g.EnableRepetitionTracking()
	p := Strong(4)
	for i := 0; i < 80 && !g.IsOver(); i++ {
		m, ok := PlayerPick(p, g)
		if !ok {
			break
		}
		g.ApplyMove(m.From, m.To)
		if i >= 6 && i%3 == 0 {
			c := *g
			positions = append(positions, &c)
		}
	}

	for _, depth := range []int{4, 5, 6} {
		differing, worst, total := 0, 0.0, 0.0
		for _, pos := range positions {
			on, off := Strong(depth), Strong(depth)
			on.Aspiration, off.Aspiration = true, false
			sOn, ok1 := PlayerScoreWith(on, pos, nil)
			sOff, ok2 := PlayerScoreWith(off, pos, nil)
			if !ok1 || !ok2 {
				continue
			}
			d := math.Abs(sOn - sOff)
			total += d
			if d > 1e-6 {
				differing++
				if d > worst {
					worst = d
				}
			}
		}
		t.Logf("depth %d: %d of %d positions score differently, worst %.3f pawns, mean %.4f",
			depth, differing, len(positions), worst, total/float64(len(positions)))
		if differing > 0 {
			t.Errorf("depth %d: %d of %d positions returned a different score for the "+
				"same position and configuration, worst by %.3f pawns. Something is "+
				"carrying over between searches.", depth, differing, len(positions), worst)
		}
	}
}
