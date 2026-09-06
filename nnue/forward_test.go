package main

import (
	"math"
	"math/rand"
	"testing"

	"chess/board"
	"chess/engine"
	"chess/game"
)

// The same network is evaluated by two separate pieces of code: `forward`
// here, which is what training fits, and `HalfKPNet.Evaluate` in the
// engine, which is what plays. Nothing until now checked that they agree.
//
// A divergence between them is the worst kind of bug in this pipeline,
// because every training number stays healthy while the engine plays a
// different function from the one that was fitted. The units bug earlier
// in this project was exactly that shape and cost several hundred Elo
// across every experiment before it was found.
func TestTrainingAndSearchForwardPassesAgree(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	n := newNet(16, rng)
	// Random weights rather than the initial ones, so the test would catch
	// an error that cancels at initialisation.
	for i := range n.w1 {
		n.w1[i] = float32(rng.NormFloat64() * 0.05)
	}
	for i := range n.b1 {
		n.b1[i] = float32(rng.NormFloat64() * 0.3)
	}
	for i := range n.w2 {
		n.w2[i] = float32(rng.NormFloat64() * 0.5)
	}
	n.b2 = float32(rng.NormFloat64())

	exported := n.export(0.30, false)

	fens := []string{
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"8/8/4k3/8/8/8/8/K6R w - - 0 1",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
	}
	for _, fen := range fens {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		var s sample
		s.own = engine.AppendHalfKPFeatures(nil, &g.Board, board.White)
		s.opp = engine.AppendHalfKPFeatures(nil, &g.Board, board.Black)

		acc := make([]float32, 2*n.h)
		act := make([]float32, 2*n.h)
		train := float64(n.forward(&s, acc, act))
		search := exported.Evaluate(&g.Board)

		if math.Abs(train-search) > 1e-4 {
			t.Errorf("%s\n  training forward pass %.6f\n  search forward pass   %.6f\n"+
				"  the network being fitted is not the network being played",
				fen, train, search)
		}
	}
}
