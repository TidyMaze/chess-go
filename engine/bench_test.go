package engine

import (
	"chess/board"
	"chess/game"
	"testing"
)

// midOpeningPosition is the same position used to profile the Python
// engine all session (1.e4 e5 2.Nf3 Nc6-ish), so the two are comparable.
func midOpeningPosition() *game.Game {
	g := game.New()
	for _, m := range []struct{ from, to board.Sq }{
		{board.Sq{4, 1}, board.Sq{4, 3}},
		{board.Sq{4, 6}, board.Sq{4, 4}},
		{board.Sq{6, 0}, board.Sq{5, 2}},
		{board.Sq{1, 7}, board.Sq{2, 5}},
	} {
		g.ApplyMove(m.from, m.to)
	}
	return g
}

func BenchmarkChooseMoveDepth3(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := midOpeningPosition()
		ChooseMove(g, g.Turn, 3, nil)
	}
}

func BenchmarkChooseMoveDepth5(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := midOpeningPosition()
		ChooseMove(g, g.Turn, 5, nil)
	}
}

func BenchmarkAllLegalMoves(b *testing.B) {
	g := midOpeningPosition()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.AllLegalMoves(g.Turn)
	}
}

func BenchmarkFullEngineDepth5(b *testing.B) {
	p := Player{Depth: 5, UsePST: true, Quiescence: true, TTBits: 20,
		NullMove: true, Tapered: true, Iterative: true,
		Extensions: true, Aspiration: true, SEEPruning: true}
	for i := 0; i < b.N; i++ {
		g := midOpeningPosition()
		PlayerPick(p, g)
	}
}

func BenchmarkFullEngineDepth5PersistentTT(b *testing.B) {
	p := Player{Depth: 5, UsePST: true, Quiescence: true, TTBits: 20,
		Table: NewTranspositionTable(20),
		NullMove: true, Tapered: true, Iterative: true,
		Extensions: true, Aspiration: true, SEEPruning: true}
	for i := 0; i < b.N; i++ {
		g := midOpeningPosition()
		PlayerPick(p, g)
	}
}

func BenchmarkPlaySimulation(b *testing.B) {
	p1 := Player{Depth: 2, TTBits: 14, Quiescence: true}
	p2 := Player{Depth: 2, TTBits: 14, Quiescence: true}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := game.New()
		_, _ = playFrom(g, p1, p2, 20, nil)
	}
}

