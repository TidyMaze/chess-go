package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// Static exchange evaluation: what a capture (or a quiet move) is worth
// once every recapture on that square has been played out, least
// valuable attacker first, in pawns. Positions are small enough to work
// out by hand; each exercises a branch of the swap-off.
func TestStaticExchangeEvaluation(t *testing.T) {
	cases := []struct {
		name string
		fen  string
		move game.Move
		want int
	}{
		{"pawn takes an undefended knight", "4k3/8/8/3n4/4P3/8/8/4K3 w - - 0 1", mv(4, 3, 3, 4), 3},
		{"queen takes a pawn defended by a pawn", "4k3/8/2p5/3p4/4Q3/8/8/4K3 w - - 0 1", mv(4, 3, 3, 4), -8},
		{"rook trade backed by an x-ray rook", "3r3k/3r4/8/8/8/8/3R4/3RK3 w - - 0 1", mv(3, 1, 3, 6), 5},
		{"the defending king cannot recapture into check", "8/8/3k4/4n3/3P4/8/8/4RK2 w - - 0 1", mv(3, 3, 4, 4), 3},
		{"king recaptures when nothing guards the square", "3rk3/3r4/8/8/8/8/3R4/3RK3 w - - 0 1", mv(3, 1, 3, 6), 0},
		{"en passant is a pawn capture", "4k3/8/8/3pP3/8/8/8/4K3 w - d6 0 2", mv(4, 4, 3, 5), 1},
		{"quiet move onto a pawn-attacked square", "4k3/8/8/3p4/8/2N5/8/4K3 w - - 0 1", mv(2, 2, 4, 3), -3},
		{"quiet move onto an attacked but defended square", "4k3/8/8/3p4/8/2NP4/8/4K3 w - - 0 1", mv(2, 2, 4, 3), -2},
	}
	for _, c := range cases {
		g, err := game.ParseFEN(c.fen)
		if err != nil {
			t.Fatal(c.name, err)
		}
		if got := see(&g.Board, c.move); got != c.want {
			t.Errorf("%s: SEE %d, want %d", c.name, got, c.want)
		}
	}
}

// The attacker scan returns the cheapest piece first, of every type.
func TestLeastValuableAttackerByType(t *testing.T) {
	cases := []struct {
		fen  string
		want board.Sq
		typ  board.PieceType
	}{
		{"4k3/8/8/3p4/4P3/8/8/4K3 w - - 0 1", board.Sq{File: 4, Rank: 3}, board.Pawn},
		{"4k3/8/8/3p4/8/2N5/8/4K3 w - - 0 1", board.Sq{File: 2, Rank: 2}, board.Knight},
		{"4k3/8/8/3p4/8/1B6/8/4K3 w - - 0 1", board.Sq{File: 1, Rank: 2}, board.Bishop},
		{"4k3/8/8/3p4/8/8/8/3RK3 w - - 0 1", board.Sq{File: 3, Rank: 0}, board.Rook},
		{"4k3/8/8/3p4/8/8/8/3QK3 w - - 0 1", board.Sq{File: 3, Rank: 0}, board.Queen},
		{"4k3/8/8/3p4/3K4/8/8/8 w - - 0 1", board.Sq{File: 3, Rank: 3}, board.King},
	}
	target := board.Sq{File: 3, Rank: 4} // d5
	for _, c := range cases {
		g, _ := game.ParseFEN(c.fen)
		sq, typ, ok := leastValuableAttacker(&g.Board, target, board.White)
		if !ok || sq != c.want || typ != c.typ {
			t.Errorf("%s: attacker %v %v %v, want %v %v", c.fen, sq, typ, ok, c.want, c.typ)
		}
	}
	// Several attackers: the pawn before the knight before the rook.
	g, _ := game.ParseFEN("4k3/8/8/3p4/4P3/2N5/8/3RK3 w - - 0 1")
	if _, typ, _ := leastValuableAttacker(&g.Board, target, board.White); typ != board.Pawn {
		t.Errorf("with pawn, knight and rook attacking, got %v first", typ)
	}
	// Nobody attacks: reported as such.
	if _, _, ok := leastValuableAttacker(&g.Board, board.Sq{File: 7, Rank: 7}, board.White); ok {
		t.Error("h8 is attacked by nobody and an attacker was reported")
	}
}

func TestSEEOfANonMoveIsZero(t *testing.T) {
	g, _ := game.ParseFEN("4k3/8/8/8/8/8/8/4K3 w - - 0 1")
	if v := see(&g.Board, mv(0, 0, 1, 1)); v != 0 {
		t.Errorf("moving nothing scored %d", v)
	}
}

func TestSEEPruningRule(t *testing.T) {
	if !seePrunes(3, -4, false, false, true) {
		t.Error("losing four pawns at depth 3 must prune")
	}
	if seePrunes(3, -3, false, false, true) {
		t.Error("losing exactly depth pawns is kept")
	}
	if seePrunes(7, -20, false, false, true) {
		t.Error("never past depth 6")
	}
	if seePrunes(3, -9, true, false, true) || seePrunes(3, -9, false, true, true) || seePrunes(3, -9, false, false, false) {
		t.Error("never in check, never a checking capture, never on an open window")
	}
}

// Wired in, the search must search fewer nodes with losing captures
// pruned and captures ordered by their exchange value.
func TestMainSearchSEECutsNodes(t *testing.T) {
	var with, without int
	for _, fen := range correctnessPositions[:6] {
		g, _ := game.ParseFEN(fen)
		off, on := Strong(6), Strong(6)
		off.MainSEE = false
		on.MainSEE = true
		ResetNodes()
		PlayerScoreWith(off, g, nil)
		without += TotalNodes()
		ResetNodes()
		PlayerScoreWith(on, g, nil)
		with += TotalNodes()
	}
	t.Logf("nodes: %d without main-search SEE, %d with (%.2fx)", without, with, float64(without)/float64(with))
	if with >= without {
		t.Errorf("SEE in the main search removed no nodes: %d with, %d without", with, without)
	}
}

// TestDeltaPruningCutsNodes asserts that delta pruning in quiescence search
// drops node count while retaining valid legal moves.
func TestDeltaPruningCutsNodes(t *testing.T) {
	var with, without int
	for _, fen := range correctnessPositions[:6] {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		off, on := Strong(5), Strong(5)
		off.DeltaPruning = false
		on.DeltaPruning = true

		ResetNodes()
		mOff, ok1 := PlayerPick(off, g)
		without += TotalNodes()

		ResetNodes()
		mOn, ok2 := PlayerPick(on, g)
		with += TotalNodes()

		if !ok1 || !ok2 {
			t.Fatalf("PlayerPick failed on fen %s: ok1=%v, ok2=%v", fen, ok1, ok2)
		}
		if mOff.From == mOff.To || mOn.From == mOn.To {
			t.Errorf("empty move picked on fen %s: off=%v, on=%v", fen, mOff, mOn)
		}
	}
	t.Logf("nodes: %d without delta pruning, %d with (%.2fx)", without, with, float64(without)/float64(with))
	if with >= without {
		t.Errorf("delta pruning did not reduce nodes: without=%d, with=%d", without, with)
	}
}

