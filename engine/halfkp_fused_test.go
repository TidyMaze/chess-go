package engine

import (
	"math"
	"math/rand"
	"testing"

	"chess/board"
	"chess/game"
)

// The fused update reads the parent and writes the child in one pass instead
// of a copy plus one pass per changed piece. Each element must still see the
// same operations in the same order, so the result is identical to the bit,
// not to a tolerance: golden evaluations and node counts depend on it.
func TestFusedDeltaIsBitIdenticalToCopyThenApply(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network here:", err)
	}
	h, buckets := net.H, net.buckets()
	rng := rand.New(rand.NewSource(11))
	compared := 0
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		var parent, child halfKPAcc
		net.refresh(&g.Board, &parent, nil, nil)
		color := g.Turn
		for ply := 0; ply < 16; ply++ {
			legal := g.AllLegalMoves(color)
			if len(legal) == 0 {
				break
			}
			makeSearchMove(g, legal[rng.Intn(len(legal))])
			color = color.Other()
			net.refresh(&g.Board, &child, nil, nil) // snapshot of pieces and kings
			for side, persp := range [2]board.Color{board.White, board.Black} {
				if perspectiveKingSlot(parent.kings[side], persp, buckets) != perspectiveKingSlot(child.kings[side], persp, buckets) {
					continue
				}
				want := make([]float32, h)
				copy(want, parent.acc[side][:h])
				net.applyDelta(want, &parent, &child, persp, buckets)
				got := make([]float32, h)
				if !net.fusedDelta(got, parent.acc[side][:h], &parent, &child, persp, buckets) {
					continue
				}
				for i := range want {
					if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
						t.Fatalf("%s ply %d unit %d: fused %v, copy then apply %v", fen, ply, i, got[i], want[i])
					}
				}
				compared++
			}
			parent = child
		}
	}
	if compared == 0 {
		t.Fatal("no incremental update was compared")
	}
}
