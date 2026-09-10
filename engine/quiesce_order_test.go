package engine

import (
	"os"
	"testing"

	"chess/game"
)

// Fixed-depth node count over the correctness positions, with the
// champion's ordering switches on. Fixed depth, so the number is exact and
// does not move with machine load; run it before and after a change to the
// search and the difference is the change's effect on the tree.
func TestFixedDepthNodeCount(t *testing.T) {
	if os.Getenv("MEASURE") == "" {
		t.Skip("MEASURE=1 prints the fixed-depth node count")
	}
	total := 0
	for _, fen := range correctnessPositions[3:8] {
		// Seeded: the tie-break among equal root moves is random and moves
		// the count by a couple of percent otherwise.
		SeedRandom(1)
		g, _ := game.ParseFEN(fen)
		p := Strong(5)
		p.MainSEE = true
		ResetNodes()
		PlayerPick(p, g)
		total += TotalNodes()
	}
	t.Logf("depth 5 over %d positions: %d nodes", len(correctnessPositions[3:8]), total)
}
