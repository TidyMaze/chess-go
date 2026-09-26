package game

import (
	"testing"

	"chess/board"
)

// An underpromotion played by someone else (a lichess opponent, a GUI,
// Stockfish, a PGN) has to land on the board as the piece they chose.
// Replaying it as a queen leaves every later position wrong: in game
// ymteJEvj the opponent's f8=N gave check, the bot saw a queen that did
// not, and it posted an illegal move and lost on time.
func TestUCIUnderpromotionIsAppliedAsThePieceChosen(t *testing.T) {
	for _, tc := range []struct {
		uci  string
		want board.PieceType
	}{
		{"e7e8n", board.Knight},
		{"e7e8b", board.Bishop},
		{"e7e8r", board.Rook},
		{"e7e8q", board.Queen},
		{"e7e8", board.Queen},
	} {
		g, err := ParseFEN("8/4P1k1/8/8/8/8/6K1/8 w - - 0 1")
		if err != nil {
			t.Fatal(err)
		}
		m, ok := MoveFromUCI(tc.uci)
		if !ok {
			t.Fatalf("%s did not parse", tc.uci)
		}
		g.Apply(m)
		p, ok := g.Board.PieceAt(board.Sq{File: 4, Rank: 7})
		if !ok || p != (board.Piece{Color: board.White, Type: tc.want}) {
			t.Errorf("after %s the board has %+v (ok=%v) on e8, want a white %v", tc.uci, p, ok, tc.want)
		}
	}
}

// A queen promotion parses to the move the generator produces, so a book
// or engine "e7e8q" is still found in the legal move list; an
// underpromotion prints back with its letter.
func TestUCIPromotionRoundTrip(t *testing.T) {
	g, err := ParseFEN("8/4P1k1/8/8/8/8/6K1/8 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	q, _ := MoveFromUCI("e7e8q")
	found := false
	for _, m := range g.AllLegalMoves(board.White) {
		if m == q {
			found = true
		}
	}
	if !found {
		t.Errorf("e7e8q parsed to %+v, which is not in the legal list", q)
	}
	n, _ := MoveFromUCI("e7e8n")
	if got := n.UCI(); got != "e7e8n" {
		t.Errorf("e7e8n prints back as %q", got)
	}
}

// The PGN importer resolves SAN: "e8=N" has to come back as a knight
// promotion, not the queen the engine would have chosen.
func TestSANUnderpromotionIsAppliedAsThePieceChosen(t *testing.T) {
	g, err := ParseFEN("8/4P1k1/8/8/8/8/6K1/8 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := MoveFromSAN(g, "e8=N+")
	if !ok {
		t.Fatal("e8=N+ did not resolve")
	}
	g.Apply(m)
	p, ok := g.Board.PieceAt(board.Sq{File: 4, Rank: 7})
	if !ok || p != (board.Piece{Color: board.White, Type: board.Knight}) {
		t.Errorf("after e8=N the board has %+v (ok=%v) on e8, want a white knight", p, ok)
	}
}
