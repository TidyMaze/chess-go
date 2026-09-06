package game

import (
	"testing"

	"chess/board"
)

// Tuning needs positions on disk: generating them means playing thousands
// of self-play games, which is far too expensive to redo every time the
// fitting code changes. FEN is already the format this engine speaks to
// Stockfish, so round-tripping through it is the cheapest persistence
// available.
func TestParseFENRoundTrips(t *testing.T) {
	positions := []*Game{
		New(),
		func() *Game {
			g := New()
			for _, uci := range []string{"e2e4", "e7e5", "g1f3", "b8c6", "f1c4"} {
				m, _ := MoveFromUCI(uci)
				g.ApplyMove(m.From, m.To)
			}
			return g
		}(),
		func() *Game {
			b := board.NewEmpty()
			b.Place(board.Sq{4, 0}, board.Piece{board.White, board.King})
			b.Place(board.Sq{0, 4}, board.Piece{board.White, board.Rook})
			b.Place(board.Sq{4, 4}, board.Piece{board.Black, board.King})
			return From(b, board.Black)
		}(),
	}

	for i, g := range positions {
		want := g.FEN()
		got, err := ParseFEN(want)
		if err != nil {
			t.Fatalf("position %d: ParseFEN(%q): %v", i, want, err)
		}
		if again := got.FEN(); again != want {
			t.Errorf("position %d: round trip changed the FEN\n  in:  %s\n  out: %s", i, want, again)
		}
		if got.Turn != g.Turn {
			t.Errorf("position %d: side to move %v, want %v", i, got.Turn, g.Turn)
		}
	}
}

func TestParseFENRejectsGarbage(t *testing.T) {
	for _, s := range []string{
		"",
		"not a fen",
		"8/8/8/8/8/8/8/8",           // no side to move
		"8/8/8/8/8/8/8/8 x - - 0 1", // bad side
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w - - 0",   // too few fields
		"9/8/8/8/8/8/8/8 w - - 0 1",                             // rank overflows
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNX w - - 0 1", // bad piece letter
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP w - - 0 1",          // too few ranks
	} {
		if _, err := ParseFEN(s); err == nil {
			t.Errorf("ParseFEN(%q) accepted invalid input", s)
		}
	}
}

// A position with more than 32 pieces is a variant, not a standard one.
// The Lichess evaluation dump contains them (Horde opens with 36 pawns,
// Crazyhouse drops captured pieces back in). Before this check they ran
// off the end of the board's 32-entry occupied list and panicked, and
// since ParseFEN also validates positions arriving from the browser, that
// panic was reachable from an HTTP request.
func TestParseFENRejectsMoreThan32Pieces(t *testing.T) {
	for _, tc := range []struct{ name, fen string }{
		{"horde", "7K/PPPPPPPP/PPPPPPPP/PPPPPPPP/PPPPPPPP/PPPPPPPP/PPPPPPPP/q6k b - - 0 1"},
		{"crazyhouse", "rnbqk1nr/1pp2ppp/pbnp4/3Pp3/B3P3/2P2N2/PP3PPP/RNBQKBNR b KQkq - 0 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseFEN(tc.fen); err == nil {
				t.Error("accepted a position with more than 32 pieces")
			}
		})
	}
}

func TestParseFENStillAcceptsAFullBoard(t *testing.T) {
	if _, err := ParseFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"); err != nil {
		t.Errorf("the starting position has exactly 32 pieces and must parse: %v", err)
	}
}
