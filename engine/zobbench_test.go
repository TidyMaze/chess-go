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
