package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

func TestPassedPawnScoreIncreasesWithRank(t *testing.T) {
	// White passed pawn on d4 vs White passed pawn on d6
	g4, err := game.ParseFEN("4k3/8/8/8/3P4/8/8/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	g6, err := game.ParseFEN("4k3/8/3P4/8/8/8/8/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}

	score4 := passedPawnScore(&g4.Board, board.White, 0.5)
	score6 := passedPawnScore(&g6.Board, board.White, 0.5)

	if score4 <= 0 {
		t.Errorf("expected positive score for passed pawn on d4, got %.3f", score4)
	}
	if score6 <= score4 {
		t.Errorf("expected d6 passed pawn (%.3f) to score higher than d4 (%.3f)", score6, score4)
	}
}

func TestPassedPawnEvaluatesOutsidePassedPawn8NmPGEfJ(t *testing.T) {
	// 8NmPGEfJ: after 24.g4 hxg4 25.h5 White creates an outside passed pawn on h5
	g, err := game.ParseFEN("3b4/1kb2p2/4pp2/p6P/5P2/4B3/PP3KP1/8 w - - 0 25")
	if err != nil {
		t.Fatal(err)
	}
	phase := gamePhase(&g.Board)
	wPasser := passedPawnScore(&g.Board, board.White, phase)
	bPasser := passedPawnScore(&g.Board, board.Black, phase)
	netPasser := wPasser - bPasser

	if netPasser <= 0.25 {
		t.Errorf("expected strong passed pawn advantage for White on h5, got %.3f", netPasser)
	}
}

func BenchmarkChampionSearchWithPassers(b *testing.B) {
	c := ReadChampion("../champion.json")
	c.NetFile = "../champion_net.json"
	c.Book = ""
	p, err := c.PlayerOrError()
	if err != nil {
		b.Skipf("champion: %v", err)
	}
	p.ApplyFeatures("passers")
	p.TimeBudget, p.Threads, p.Depth = 0, 1, 8
	nodes := 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, fen := range championBenchFENs {
			g, err := game.ParseFEN(fen)
			if err != nil {
				b.Fatal(err)
			}
			SeedRandom(1)
			ResetNodes()
			PlayerPick(p, g)
			nodes += TotalNodes()
		}
	}
	b.ReportMetric(float64(nodes)/b.Elapsed().Seconds()/1000, "knps")
	b.ReportMetric(float64(nodes)/float64(b.N), "nodes/op")
}
