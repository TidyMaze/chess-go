package engine

import (
	"fmt"
	"testing"
	"time"

	"chess/board"
	"chess/game"
)

func BenchmarkHandEvaluation(b *testing.B) {
	bd := board.Initial()
	ev := &Eval{
		UsePST:    true,
		Tapered:   true,
		Structure: true,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = PositionScoreEval(&bd, board.White, ev)
	}
}

func BenchmarkHalfKPNeuralNetwork(b *testing.B) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		b.Skipf("cannot load champion_net.json: %v", err)
	}
	bd := board.Initial()
	ev := &Eval{
		HalfKP:      net,
		HalfKPBlend: 0.0, // 100% Neural Network
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = PositionScoreEval(&bd, board.White, ev)
	}
}

func BenchmarkHalfKPHead(b *testing.B) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		b.Skipf("cannot load champion_net.json: %v", err)
	}
	h := net.H
	own := make([]float32, h)
	opp := make([]float32, h)
	for i := range own {
		own[i] = float32(i) * 0.02
		opp[i] = float32(i) * 0.015
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = net.head(own, opp)
	}
}

func BenchmarkHalfKPAddRow(b *testing.B) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		b.Skipf("cannot load champion_net.json: %v", err)
	}
	a := make([]float32, net.H)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		net.addRow(a, 100, 1.0)
	}
}

func BenchmarkChampionHybrid(b *testing.B) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		b.Skipf("cannot load champion_net.json: %v", err)
	}
	bd := board.Initial()
	ev := &Eval{
		HalfKP:      net,
		HalfKPBlend: 0.45, // Hybrid
		UsePST:      true,
		Tapered:     true,
		Structure:   true,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = PositionScoreEval(&bd, board.White, ev)
	}
}

func TestBenchmarkNetVsHandHeadToHead(t *testing.T) {
	champ := ReadChampion("../champion.json")
	champ.NetFile = "../champion_net.json"

	// 1. Champion with Neural Network
	pNet := champ.Player()
	pNet.Name = "Champion (HalfKP Net)"
	pNet.TimeBudget = 250 * time.Millisecond
	pNet.Threads = 4

	// 2. Pure Piece-Value Table (Hand Evaluation) with identical search features
	pHand := champ.Player()
	pHand.Name = "Hand Eval (Pure Piece-Value Table)"
	pHand.HalfKP = nil
	pHand.HalfKPBlend = 1.0
	pHand.TimeBudget = 250 * time.Millisecond
	pHand.Threads = 4

	// Bench search depth and nodes in starting position
	g := game.New()
	t0 := time.Now()
	_, _ = PlayerPick(pHand, g)
	durHand := time.Since(t0)
	depthHand := LastSearchDepth()
	nodesHand := TotalNodes()
	knpsHand := float64(nodesHand) / durHand.Seconds() / 1000

	g = game.New()
	t0 = time.Now()
	_, _ = PlayerPick(pNet, g)
	durNet := time.Since(t0)
	depthNet := LastSearchDepth()
	nodesNet := TotalNodes()
	knpsNet := float64(nodesNet) / durNet.Seconds() / 1000

	t.Logf("=== 250ms Search Performance in Root ===")
	t.Logf("Hand Eval: depth=%d nodes=%d elapsed=%v knps=%.0f", depthHand, nodesHand, durHand, knpsHand)
	t.Logf("Neural Net: depth=%d nodes=%d elapsed=%v knps=%.0f", depthNet, nodesNet, durNet, knpsNet)

	// Play a match between them (8 games, alternating colors, 250ms/move)
	games := 8
	maxMoves := 120
	t.Logf("Playing %d match games: %s vs %s...", games, pNet.Name, pHand.Name)
	res := PlayMatch(pNet, pHand, games, maxMoves)
	t.Logf("=== Head-to-Head Match Result ===")
	t.Logf("Champion Net: Wins=%d Draws=%d Losses=%d (Score: %.2f / %d)", res.Wins, res.Draws, res.Losses, res.Score(), res.Games())
	t.Logf("Estimated Elo Advantage for Neural Net: %+d Elo", res.Elo())
	fmt.Printf("\n[RESULT] Champion Net vs Hand Eval: Wins=%d Draws=%d Losses=%d, Elo Gap=%+d\n", res.Wins, res.Draws, res.Losses, res.Elo())
}
