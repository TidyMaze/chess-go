package engine

import (
	"math"
	"math/rand"
	"testing"

	"chess/board"
	"chess/game"
)

// The incremental accumulator must agree with a full recomputation.
//
// A move changes two to four features per perspective; recomputing all
// sixty rows per node was 21% of the engine's CPU. The incremental path
// copies the parent's accumulator and applies only the rows that changed,
// so float32 sums land in a different order and the agreement is to a
// tolerance, not to the bit. It must also actually take the incremental
// path: a stub that recomputes in full would pass the equality half.
func TestIncrementalAccumulatorMatchesFullEvaluation(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network here:", err)
	}
	rng := rand.New(rand.NewSource(7))
	worst := 0.0
	var stats halfKPAccStats
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		var stack [12]halfKPAcc
		color := g.Turn
		// Root: nothing to be incremental from.
		full := net.Evaluate(&g.Board)
		inc := net.EvaluateWith(&g.Board, &stack[0], nil, &stats)
		if d := math.Abs(full - inc); d > worst {
			worst = d
		}
		// Walk a random line, each ply evaluated from its parent's slot.
		var undos []board.Undo
		for ply := 1; ply < len(stack); ply++ {
			legal := g.AllLegalMoves(color)
			if len(legal) == 0 {
				break
			}
			m := legal[rng.Intn(len(legal))]
			undo, _ := makeSearchMove(g, m)
			undos = append(undos, undo)
			color = color.Other()
			stack[ply].valid = false
			full := net.Evaluate(&g.Board)
			inc := net.EvaluateWith(&g.Board, &stack[ply], &stack[ply-1], &stats)
			if d := math.Abs(full - inc); d > worst {
				worst = d
			}
		}
		for i := len(undos) - 1; i >= 0; i-- {
			g.Board.UnmakeMove(undos[i])
		}
	}
	t.Logf("worst disagreement %.2e pawns; %d incremental updates, %d full refreshes",
		worst, stats.incremental, stats.full)
	if worst > 1e-4 {
		t.Errorf("incremental accumulator disagrees with the full evaluation by %.2e pawns", worst)
	}
	if stats.incremental == 0 {
		t.Error("no incremental update happened: every node was recomputed in full")
	}
}
