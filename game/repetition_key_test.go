package game

import (
	"strings"
	"testing"
)

func playUCI(t *testing.T, g *Game, moves string) {
	t.Helper()
	for _, s := range strings.Fields(moves) {
		m, ok := MoveFromUCI(s)
		if !ok {
			t.Fatalf("bad move %q", s)
		}
		g.Apply(m)
	}
}

// 1.e4 e5 2.Ke2 Ke7 3.Ke1 Ke8 4.Ke2 Ke7 5.Ke1 Ke8: the pieces stand as
// after 1...e5 three times, but the first time both sides could still
// castle. FIDE counts two occurrences, so the game goes on; harness and
// training games were ending here as draws.
func TestThreefoldCountsCastlingRights(t *testing.T) {
	g := New()
	playUCI(t, g, "e2e4 e7e5 e1e2 e8e7 e2e1 e7e8 e1e2 e8e7 e2e1 e7e8")
	if g.IsThreefoldRepetition() {
		t.Fatal("declared a threefold: the position with castling rights (after 1...e5) was counted with the two without")
	}
	playUCI(t, g, "e1e2 e8e7 e2e1 e7e8")
	if !g.IsThreefoldRepetition() {
		t.Error("the third occurrence without castling rights is a threefold")
	}
}

// After d7-d5 the e5 pawn can take en passant, so that position differs
// from the same pieces two knight shuffles later, when it no longer can.
func TestThreefoldCountsAUsableEnPassantSquare(t *testing.T) {
	g, err := ParseFEN("4k1n1/3p4/8/4P3/8/8/8/4K1N1 b - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	g.EnableRepetitionTracking()
	playUCI(t, g, "d7d5 g1f3 g8f6 f3g1 f6g8 g1f3 g8f6 f3g1 f6g8")
	if g.IsThreefoldRepetition() {
		t.Fatal("declared a threefold: the position with en passant available was counted with the two without")
	}
	playUCI(t, g, "g1f3 g8f6 f3g1 f6g8")
	if !g.IsThreefoldRepetition() {
		t.Error("the third occurrence without en passant is a threefold")
	}
}

// After a7-a5 no white pawn can take, so the square the board records is
// not part of the position: the two knight shuffles make a threefold.
func TestThreefoldIgnoresAnUnusableEnPassantSquare(t *testing.T) {
	g, err := ParseFEN("4k1n1/p7/8/8/8/8/8/4K1N1 b - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	g.EnableRepetitionTracking()
	playUCI(t, g, "a7a5 g1f3 g8f6 f3g1 f6g8 g1f3 g8f6 f3g1 f6g8")
	if !g.IsThreefoldRepetition() {
		t.Error("third occurrence missed: the en passant square after a7-a5, which no pawn can use, split the count")
	}
}

// The e5 pawn is pinned to its king by the e8 rook, so exd6 e.p. is
// illegal after d7-d5: FIDE, lichess and python-chess count the three
// occurrences as one position.
func TestThreefoldIgnoresAnEnPassantSquareOnlyAPinnedPawnCouldUse(t *testing.T) {
	g, err := ParseFEN("4r1k1/3p4/8/4P3/8/8/8/4K1N1 b - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	g.EnableRepetitionTracking()
	playUCI(t, g, "d7d5 g1f3 g8h8 f3g1 h8g8 g1f3 g8h8 f3g1 h8g8")
	if !g.IsThreefoldRepetition() {
		t.Error("third occurrence missed: exd6 e.p. is illegal (pinned pawn), yet the en passant square split the count")
	}
}
