package engine

import "testing"

// What does the aspiration window cost at the depth actually played?
//
// It was written as a speed optimisation: search a narrow window around
// the previous iteration's score, and re-search on a fail. Bisecting a
// depth-6-loses-to-depth-5 regression put the whole 174 Elo of it here,
// so the question is no longer whether it is free but how much it costs
// where the engine plays.
func TestAspirationCostAtPlayingDepth(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	for _, d := range []int{4, 5, 6} {
		off, on := Strong(d), Strong(d)
		off.Aspiration = false
		off.Name, on.Name = "no aspiration", "aspiration"
		res := PlayMatch(off, on, 400, 250)
		t.Logf("depth %d: aspiration off vs on: W-D-L %d-%d-%d  Elo %+d +/- %d",
			d, res.Wins, res.Draws, res.Losses, res.Elo(), res.EloMargin())
	}
}
