package game

import (
	"strings"
	"testing"
)

// Replaying a real game is the whole point: if SAN resolution is wrong the
// importer silently trains on positions that were never played.
func TestSANReplaysAFullGame(t *testing.T) {
	// Opera Game, Morphy against Duke Karl and Count Isouard, 1858. Chosen
	// because it exercises long castling, pawn and piece captures, a
	// disambiguated knight move, checks, a queen sacrifice and mate.
	moves := strings.Fields(`e4 e5 Nf3 d6 d4 Bg4 dxe5 Bxf3 Qxf3 dxe5 Bc4 Nf6
		Qb3 Qe7 Nc3 c6 Bg5 b5 Nxb5 cxb5 Bxb5+ Nbd7 O-O-O Rd8
		Rxd7 Rxd7 Rd1 Qe6 Bxd7+ Nxd7 Qb8+ Nxb8 Rd8#`)
	g := New()
	for i, san := range moves {
		m, ok := MoveFromSAN(g, san)
		if !ok {
			t.Fatalf("move %d (%q) did not resolve; position %s", i+1, san, g.FEN())
		}
		g.ApplyMove(m.From, m.To)
	}
	// The game ends in mate, which is the strongest check that all 33
	// moves resolved to the right squares: one wrong move and the final
	// position is not mate.
	if !g.IsCheckmate(g.Turn) {
		t.Errorf("final position is not checkmate, so the replay diverged: %s", g.FEN())
	}
}

func TestSANCastling(t *testing.T) {
	for _, tc := range []struct{ name, fen, san, want string }{
		{"white short", "r1bqk2r/pppp1ppp/2n2n2/2b1p3/2B1P3/2N2N2/PPPP1PPP/R1BQK2R w KQkq - 0 5", "O-O", "e1g1"},
		{"white long", "r3kbnr/pppqpppp/2np4/8/8/2NPB3/PPPQPPPP/R3KBNR w KQkq - 0 6", "O-O-O", "e1c1"},
		{"black short", "r1bqk2r/pppp1ppp/2n2n2/2b1p3/2B1P3/2N2N2/PPPP1PPP/R1BQ1RK1 b kq - 0 5", "O-O", "e8g8"},
		{"zero notation", "r1bqk2r/pppp1ppp/2n2n2/2b1p3/2B1P3/2N2N2/PPPP1PPP/R1BQK2R w KQkq - 0 5", "0-0", "e1g1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := ParseFEN(tc.fen)
			if err != nil {
				t.Fatal(err)
			}
			m, ok := MoveFromSAN(g, tc.san)
			if !ok {
				t.Fatalf("%q did not resolve", tc.san)
			}
			if m.UCI() != tc.want {
				t.Errorf("%q gave %s, want %s", tc.san, m.UCI(), tc.want)
			}
		})
	}
}

// Two knights able to reach the same square is where SAN needs a hint, and
// where a parser that ignores the hint quietly plays the wrong piece.
func TestSANDisambiguation(t *testing.T) {
	// Knights on b1 and f3, both able to reach d2.
	g, err := ParseFEN("4k3/8/8/8/8/5N2/8/1N2K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := MoveFromSAN(g, "Nd2"); ok {
		t.Error("ambiguous Nd2 resolved; it must be refused rather than guessed")
	}
	m, ok := MoveFromSAN(g, "Nbd2")
	if !ok || m.UCI() != "b1d2" {
		t.Errorf("Nbd2 gave %v %v, want b1d2", m.UCI(), ok)
	}
	m, ok = MoveFromSAN(g, "Nfd2")
	if !ok || m.UCI() != "f3d2" {
		t.Errorf("Nfd2 gave %v %v, want f3d2", m.UCI(), ok)
	}
}

func TestSANStripsCheckAndAnnotationMarks(t *testing.T) {
	g, _ := ParseFEN("4k3/8/8/8/8/8/8/R3K3 w - - 0 1")
	for _, san := range []string{"Ra8+", "Ra8#", "Ra8!", "Ra8!?"} {
		m, ok := MoveFromSAN(g, san)
		if !ok || m.UCI() != "a1a8" {
			t.Errorf("%q gave %v %v, want a1a8", san, m.UCI(), ok)
		}
	}
}

func TestSANPromotionAndCapture(t *testing.T) {
	g, err := ParseFEN("1n2k3/P7/8/8/8/8/8/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := MoveFromSAN(g, "axb8=Q")
	if !ok || m.UCI() != "a7b8" {
		t.Errorf("axb8=Q gave %v %v, want a7b8", m.UCI(), ok)
	}
}

// A token that does not describe a legal move must be refused, not
// resolved to something nearby: the game is then skipped, which is far
// cheaper than importing positions from a line nobody played.
func TestSANRefusesNonsense(t *testing.T) {
	g := New()
	for _, san := range []string{"", "zz", "Qxh7", "e9", "Ke2", "x"} {
		if m, ok := MoveFromSAN(g, san); ok {
			t.Errorf("%q resolved to %s from the starting position", san, m.UCI())
		}
	}
}
