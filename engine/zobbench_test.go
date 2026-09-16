package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

func BenchmarkZobristBoardMiddlegame(b *testing.B) {
	g, _ := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	for i := 0; i < b.N; i++ {
		zobristBoard(&g.Board, board.White)
	}
}

func BenchmarkZobristIncremental(b *testing.B) {
	g, _ := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	m := game.Move{From: board.Sq{File: 4, Rank: 2}, To: board.Sq{File: 4, Rank: 3}}
	key := zobristBoard(&g.Board, board.White)
	undo, prom := makeSearchMove(g, m)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = zobristUpdate(key, &g.Board, m, undo, prom)
	}
}
