package game

import (
	"strings"
	"testing"
)

func TestMoveFromUCIRejectsBadInput(t *testing.T) {
	for _, s := range []string{"", "e2e", "z9a1", "a1i1", "e2e0"} {
		if _, ok := MoveFromUCI(s); ok {
			t.Errorf("%q parsed as a move", s)
		}
	}
}

func TestParseFENRejectsMalformedRanks(t *testing.T) {
	for _, fen := range []string{
		"rnbqkbnrr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", // nine pieces on a rank
		"rnbqkbn/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",   // seven files
		"rnbqkbnr/pppppppp/9/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",  // a nine
		"8p/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",        // a piece after eight empties
	} {
		if _, err := ParseFEN(fen); err == nil {
			t.Errorf("%s parsed without error", fen)
		}
	}
}

func TestMoveFromSANRejectsWhatItCannotRead(t *testing.T) {
	start := New()
	for _, san := range []string{
		"O-O",   // castling not available at the start
		"e8=",   // promotion with no piece
		"e8=X",  // promotion to nothing
		"Xe4",   // not a piece letter
		"N?f3",  // garbage disambiguation
		"Nb1d3", // no knight on b1 can reach d3... actually b1 knight reaches d2/c3/a3
	} {
		if _, ok := MoveFromSAN(start, san); ok {
			t.Errorf("%q was accepted from the start position", san)
		}
	}
	// A rank hint that excludes the only candidate.
	g, err := ParseFEN("4k3/8/8/1N6/8/8/8/1N2K3 w - - 0 1") // knights on b1 and b5, both reach d... b1->d2? no: b1 reaches a3, c3, d2; b5 reaches d6, d4, c7, a7, c3, a3
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := MoveFromSAN(g, "N1c3"); !ok {
		t.Error("N1c3 should resolve to the b1 knight")
	}
	if _, ok := MoveFromSAN(g, "N5c3"); !ok {
		t.Error("N5c3 should resolve to the b5 knight")
	}
	if _, ok := MoveFromSAN(g, "N7c3"); ok {
		t.Error("N7c3 names a rank with no knight")
	}
	if m, ok := MoveFromSAN(g, "Nbc3"); ok && !strings.Contains("b", "b") {
		t.Errorf("unexpected %v", m)
	}
}

// An explicit P is tolerated as a pawn, the way some old scores write it.
func TestMoveFromSANToleratesAnExplicitPawnLetter(t *testing.T) {
	if _, ok := MoveFromSAN(New(), "Pe4"); !ok {
		t.Error("Pe4 was rejected")
	}
}

func TestPieceFromSANLetters(t *testing.T) {
	for _, c := range []struct {
		letter byte
		ok     bool
	}{{'K', true}, {'Q', true}, {'R', true}, {'B', true}, {'N', true}, {'P', true}, {'Z', false}, {'k', false}} {
		if _, ok := pieceFromSAN(c.letter); ok != c.ok {
			t.Errorf("%q: ok=%v, want %v", c.letter, ok, c.ok)
		}
	}
}
