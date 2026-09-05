package game

import (
	"testing"

	"chess/board"
)

// Castling was missing entirely. That is not a tuning gap but a rule the
// engine cannot follow, and it costs more than any evaluation term: a
// king stuck in the centre is exposed for the whole game and the rooks
// never connect. It also explains a measurement that looked like a
// tuning artefact, the fitter repeatedly driving the king
// piece-square table to zero: in games where nobody can castle, where
// the king stands really does not predict anything.

func hasMove(g *Game, uci string) bool {
	want, ok := MoveFromUCI(uci)
	if !ok {
		return false
	}
	for _, m := range g.AllLegalMoves(g.Turn) {
		if m == want {
			return true
		}
	}
	return false
}

// backRankOnly builds a position with both kings and the given white
// rooks, so castling can be tested without other pieces interfering.
func backRankOnly(t *testing.T, fen string) *Game {
	t.Helper()
	g, err := ParseFEN(fen)
	if err != nil {
		t.Fatalf("ParseFEN(%q): %v", fen, err)
	}
	return g
}

func TestCastlingIsGenerated(t *testing.T) {
	cases := []struct {
		name string
		fen  string
		uci  string
		want bool
	}{
		{"white kingside available", "4k3/8/8/8/8/8/8/4K2R w KQkq - 0 1", "e1g1", true},
		{"white queenside available", "4k3/8/8/8/8/8/8/R3K3 w KQkq - 0 1", "e1c1", true},
		{"black kingside available", "4k2r/8/8/8/8/8/8/4K3 b KQkq - 0 1", "e8g8", true},
		{"black queenside available", "r3k3/8/8/8/8/8/8/4K3 b KQkq - 0 1", "e8c8", true},
		{"no rights means no castling", "4k3/8/8/8/8/8/8/4K2R w - - 0 1", "e1g1", false},
		{"blocked by own piece", "4k3/8/8/8/8/8/8/4KN1R w KQkq - 0 1", "e1g1", false},
		{"queenside blocked on b1", "4k3/8/8/8/8/8/8/RN2K3 w KQkq - 0 1", "e1c1", false},
		// A king may not castle out of, through, or into check.
		{"out of check", "4k3/8/8/8/8/8/8/r3K2R w KQkq - 0 1", "e1g1", false},
		{"through an attacked square", "4k3/8/8/8/8/5r2/8/4K2R w KQkq - 0 1", "e1g1", false},
		{"into an attacked square", "4k3/8/8/8/8/6r1/8/4K2R w KQkq - 0 1", "e1g1", false},
		// The square the rook crosses may be attacked; only the king's
		// path matters. b1 attacked still allows queenside castling.
		{"rook may cross an attacked square", "4k3/8/8/8/8/1r6/8/R3K3 w KQkq - 0 1", "e1c1", true},
	}
	for _, c := range cases {
		g := backRankOnly(t, c.fen)
		if got := hasMove(g, c.uci); got != c.want {
			t.Errorf("%s: %s generated=%v, want %v", c.name, c.uci, got, c.want)
		}
	}
}

func TestCastlingMovesTheRook(t *testing.T) {
	cases := []struct {
		name, fen, uci, kingSq, rookSq string
	}{
		{"white kingside", "4k3/8/8/8/8/8/8/4K2R w KQkq - 0 1", "e1g1", "g1", "f1"},
		{"white queenside", "4k3/8/8/8/8/8/8/R3K3 w KQkq - 0 1", "e1c1", "c1", "d1"},
		{"black kingside", "4k2r/8/8/8/8/8/8/4K3 b KQkq - 0 1", "e8g8", "g8", "f8"},
		{"black queenside", "r3k3/8/8/8/8/8/8/4K3 b KQkq - 0 1", "e8c8", "c8", "d8"},
	}
	sq := func(s string) board.Sq {
		return board.Sq{File: int(s[0] - 'a'), Rank: int(s[1] - '1')}
	}
	for _, c := range cases {
		g := backRankOnly(t, c.fen)
		m, _ := MoveFromUCI(c.uci)
		g.ApplyMove(m.From, m.To)

		k, okK := g.Board.PieceAt(sq(c.kingSq))
		r, okR := g.Board.PieceAt(sq(c.rookSq))
		if !okK || k.Type != board.King {
			t.Errorf("%s: no king on %s after castling", c.name, c.kingSq)
		}
		if !okR || r.Type != board.Rook {
			t.Errorf("%s: rook did not move to %s", c.name, c.rookSq)
		}
	}
}

func TestCastlingRightsAreLost(t *testing.T) {
	// Moving the king gives up both sides; moving one rook gives up only
	// that side. Getting this wrong lets an engine castle after its rook
	// has already left, which is illegal and would corrupt the board.
	cases := []struct {
		name  string
		fen   string
		moves []string
		uci   string
		want  bool
	}{
		{"king returns home", "4k3/8/8/8/8/8/8/R3K2R w KQkq - 0 1",
			[]string{"e1e2", "e8e7", "e2e1", "e7e8"}, "e1g1", false},
		{"h-rook returns home", "4k3/8/8/8/8/8/8/R3K2R w KQkq - 0 1",
			[]string{"h1h2", "e8e7", "h2h1", "e7e8"}, "e1g1", false},
		{"h-rook moved, queenside survives", "4k3/8/8/8/8/8/8/R3K2R w KQkq - 0 1",
			[]string{"h1h2", "e8e7", "h2h1", "e7e8"}, "e1c1", true},
		{"rook captured on h1", "4k3/8/8/8/8/8/8/R3K2r w KQkq - 0 1",
			[]string{}, "e1g1", false},
	}
	for _, c := range cases {
		g := backRankOnly(t, c.fen)
		for _, u := range c.moves {
			m, ok := MoveFromUCI(u)
			if !ok {
				t.Fatalf("%s: bad move %q", c.name, u)
			}
			g.ApplyMove(m.From, m.To)
		}
		if got := hasMove(g, c.uci); got != c.want {
			t.Errorf("%s: %s generated=%v, want %v", c.name, c.uci, got, c.want)
		}
	}
}

func TestCastlingRightsRoundTripThroughFEN(t *testing.T) {
	for _, want := range []string{"KQkq", "Kq", "-", "Q", "kq"} {
		fen := "r3k2r/8/8/8/8/8/8/R3K2R w " + want + " - 0 1"
		g, err := ParseFEN(fen)
		if err != nil {
			t.Fatalf("ParseFEN(%q): %v", fen, err)
		}
		if got := g.FEN(); got != fen {
			t.Errorf("round trip changed the FEN\n  in:  %s\n  out: %s", fen, got)
		}
	}
}
