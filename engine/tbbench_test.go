package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// The probe runs at every evaluated node with four pieces or fewer. If it
// allocates, it does so millions of times per endgame move.
func BenchmarkEvalEndgameNoTablebase(b *testing.B) {
	g, _ := game.ParseFEN("8/8/4k3/8/8/8/8/K6R w - - 0 1")
	ev := &Eval{Weights: DefaultWeights(), UsePST: true, Tapered: true, Structure: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PositionScoreEval(&g.Board, board.White, ev)
	}
}

func BenchmarkEvalEndgameWithTablebase(b *testing.B) {
	tb, err := LoadTablebases("../tablebases3.bin")
	if err != nil {
		b.Skip("no tablebases")
	}
	g, _ := game.ParseFEN("8/8/4k3/8/8/8/8/K6R w - - 0 1")
	ev := &Eval{Weights: DefaultWeights(), UsePST: true, Tapered: true, Structure: true,
		Tablebases: tb}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PositionScoreEval(&g.Board, board.White, ev)
	}
}
