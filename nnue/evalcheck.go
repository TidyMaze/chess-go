package main

import (
	"encoding/json"
	"fmt"
	"os"

	"chess/board"
	"chess/engine"
	"chess/game"
)

// Cross-checking a network trained elsewhere.
//
// Training moved to PyTorch; inference stays in Go, because alpha-beta
// evaluates one position at a time and is latency-bound. That split is only
// safe if both sides agree on what the weights mean: the first layer is
// flattened feature-major, so column f must occupy w1[f*h : f*h+h], and a
// transposed export would train one function and play another while every
// training number stayed healthy.
//
// This writes what the trainer needs to check itself: real positions, the
// features this engine extracts from them, and the score this engine
// computes. `pytorch/verify.py` recomputes the score from those same
// features and requires a match.
type evalCheckCase struct {
	FEN  string  `json:"fen"`
	Own  []int32 `json:"own"`
	Opp  []int32 `json:"opp"`
	Eval float64 `json:"eval"`
}

// checkPositions spans the game: openings, a sharp middlegame, endgames,
// and both sides to move, because the perspective flip is exactly the kind
// of thing an export gets subtly wrong.
var checkPositions = []string{
	"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
	"r1bqkbnr/pppp1ppp/2n5/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R b KQkq - 3 3",
	"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
	"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
	"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	"8/8/4k3/8/8/8/8/K6R w - - 0 1",
	"6k1/8/8/8/8/8/8/QQQQK3 b - - 0 1",
	"4k3/8/8/8/8/8/8/4K2R w K - 0 1",
}

func emitEvalCheck(outPath, netPath string) error {
	n, err := engine.LoadHalfKPNet(netPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", netPath, err)
	}
	var cases []evalCheckCase
	for _, fen := range checkPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			return fmt.Errorf("%s: %w", fen, err)
		}
		var own, opp []int32
		own = engine.AppendHalfKPFeaturesN(own, &g.Board, board.White, n.Buckets, n.Mirror)
		opp = engine.AppendHalfKPFeaturesN(opp, &g.Board, board.Black, n.Buckets, n.Mirror)
		cases = append(cases, evalCheckCase{
			FEN: fen, Own: own, Opp: opp, Eval: n.Evaluate(&g.Board),
		})
	}
	data, err := json.MarshalIndent(cases, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return err
	}
	fmt.Printf("wrote %d cross-check positions to %s (net %s, %d hidden, %d buckets)\n",
		len(cases), outPath, netPath, n.H, n.Buckets)
	return nil
}
