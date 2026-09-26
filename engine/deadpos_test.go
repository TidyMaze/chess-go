package engine

import (
	"chess/board"
	"chess/game"
	"testing"
)

func TestDeadPositionCases(t *testing.T) {
	cases := []struct {
		fen  string
		want bool
		desc string
	}{
		{"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", false, "initial position"},
		{"8/8/8/4k3/8/8/4K3/8 w - - 0 1", true, "bare kings"},
		{"8/8/8/4k3/4P3/8/4K3/8 w - - 0 1", false, "king and pawn vs king"},
		{"8/8/8/4k3/4R3/8/4K3/8 w - - 0 1", false, "king and rook vs king"},
		{"8/8/8/4k3/4Q3/8/4K3/8 w - - 0 1", false, "king and queen vs king"},
		{"8/8/8/4k3/4N3/8/4K3/8 w - - 0 1", true, "king and knight vs king"},
		{"8/8/8/4k3/4B3/8/4K3/8 w - - 0 1", true, "king and bishop vs king"},
		{"8/8/8/4k3/3NN3/8/4K3/8 w - - 0 1", false, "king and two knights vs king"},
		// e4 is file 4, rank 3 -> 4+3 = 7 (light). g4 is file 6, rank 3 -> 6+3 = 9 (light).
		{"8/8/8/4k3/4B1B1/8/4K3/8 w - - 0 1", true, "king and two same-shade bishops vs king"},
		// e4 (light) and f4 (dark: file 5, rank 3 -> 5+3 = 8)
		{"8/8/8/4k3/4BB2/8/4K3/8 w - - 0 1", false, "king and two opposite-shade bishops vs king"},
		// White bishop on e4 (light), Black bishop on g6 (file 6, rank 5 -> 6+5=11 light)
		{"8/8/6b1/4k3/4B3/8/4K3/8 w - - 0 1", true, "opposing bishops same shade"},
		// White bishop on e4 (light), Black bishop on e5 (dark: file 4, rank 4 -> 4+4=8 dark)
		{"8/8/8/4b3/4B3/8/4K3/8 w - - 0 1", false, "opposing bishops opposite shade"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			g, err := game.ParseFEN(tc.fen)
			if err != nil {
				t.Fatalf("parse FEN %s: %v", tc.fen, err)
			}
			got := deadPosition(&g.Board)
			if got != tc.want {
				t.Errorf("deadPosition(%s) = %v, want %v (%s)", tc.fen, got, tc.want, tc.desc)
			}
		})
	}
}

func BenchmarkDeadPositionInitial(b *testing.B) {
	bd := board.Initial()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = deadPosition(&bd)
	}
}
