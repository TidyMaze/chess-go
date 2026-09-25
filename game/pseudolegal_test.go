package game

import (
	"math/rand"
	"testing"

	"chess/board"
	"chess/moves"
)

// legalByMakeUnmake is the slow oracle: every move is played, the mover's
// king is asked whether it stands in check, and the move is taken back.
// No shortcut, so it cannot share a bug with the generator's.
func legalByMakeUnmake(g *Game, color board.Color, m Move) bool {
	undo := g.Board.MakeMove(m.From, m.To)
	illegal := moves.IsInCheck(&g.Board, color)
	g.Board.UnmakeMove(undo)
	return !illegal
}

// checkPseudoLegal asserts, on one position, everything the lazy search
// relies on: the pseudo-legal list filtered by IsLegal is the legal list
// in the same order, IsLegal agrees with the oracle on every move, a move
// that NeedsTest says is safe is legal, and none of it moves a piece.
// It returns how many pseudo-legal moves were illegal.
func checkPseudoLegal(t *testing.T, g *Game, where string) (illegal, screened int) {
	t.Helper()
	color := g.Turn
	inCheck := moves.IsInCheck(&g.Board, color)
	before := g.Board
	pseudo, legality := g.AppendPseudoLegalMoves(nil, color, inCheck)
	want := g.AppendLegalMoves(nil, color)
	var filtered []Move
	for _, m := range pseudo {
		oracle := legalByMakeUnmake(g, color, m)
		if got := legality.IsLegal(&g.Board, m); got != oracle {
			t.Fatalf("%s: IsLegal(%s) = %v, make/unmake says %v\n%s", where, m.UCI(), got, oracle, g.FEN())
		}
		if !legality.NeedsTest(&g.Board, m) && !oracle {
			t.Fatalf("%s: %s skipped the test and is illegal\n%s", where, m.UCI(), g.FEN())
		}
		switch legality.Screen(&g.Board, m) {
		case Legal:
			if !oracle {
				t.Fatalf("%s: %s screened legal and is illegal\n%s", where, m.UCI(), g.FEN())
			}
		case Illegal:
			if oracle {
				t.Fatalf("%s: %s screened illegal and is legal\n%s", where, m.UCI(), g.FEN())
			}
			screened++
		}
		if oracle {
			filtered = append(filtered, m)
		} else {
			illegal++
		}
	}
	if g.Board != before {
		t.Fatalf("%s: the legality test left the board changed\n%s", where, g.FEN())
	}
	if len(filtered) != len(want) {
		t.Fatalf("%s: %d legal after filtering, AppendLegalMoves has %d\n%s", where, len(filtered), len(want), g.FEN())
	}
	for i := range want {
		if filtered[i] != want[i] {
			t.Fatalf("%s: move %d is %s after filtering, %s in AppendLegalMoves\n%s", where, i, filtered[i].UCI(), want[i].UCI(), g.FEN())
		}
	}
	return illegal, screened
}

// Random real games, for the same reason as the pin prefilter test: pins,
// checks and en passant have to actually occur for the test to mean
// anything, so it also counts how many illegal pseudo-legal moves it met.
func TestPseudoLegalMovesFilterToTheLegalList(t *testing.T) {
	rng := rand.New(rand.NewSource(29))
	illegal, screened := 0, 0
	tally := func(i, s int) { illegal, screened = illegal+i, screened+s }
	for gameNo := 0; gameNo < 150; gameNo++ {
		g := New()
		for ply := 0; ply < 60; ply++ {
			tally(checkPseudoLegal(t, g, "random game"))
			legal := g.AllLegalMoves(g.Turn)
			if len(legal) == 0 {
				break
			}
			m := legal[rng.Intn(len(legal))]
			g.ApplyMove(m.From, m.To)
		}
	}
	for _, p := range perftPositions {
		g, err := ParseFEN(p.fen)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		tally(checkPseudoLegal(t, g, p.name))
	}
	if illegal == 0 {
		t.Fatal("no pseudo-legal move was ever illegal, so the filter was never exercised")
	}
	if screened == 0 {
		t.Fatal("no move was ever screened out unplayed, so the check screen was never exercised")
	}
	t.Logf("%d illegal pseudo-legal moves rejected, %d of them without being played", illegal, screened)
}

// In check, a move by any man but the king has to take the checker or
// stand between it and the king, which is known before it is played. In
// double check nothing but the king can move at all. The en passant
// capture of a checking pawn lands on neither square, so it is played.
func TestScreenRejectsWhatLeavesTheCheck(t *testing.T) {
	cases := []struct {
		name, fen string
		want      map[string]Verdict
	}{
		{"rook check", "4k3/4r3/8/8/8/8/8/R3K1N1 w - - 0 1", map[string]Verdict{
			"a1a2": Illegal, "a1b1": Illegal, "g1e2": Legal, "e1d1": Unknown,
		}},
		{"knight check", "4k3/8/8/8/8/3n4/8/R3K3 w - - 0 1", map[string]Verdict{
			"a1a2": Illegal, "a1d1": Illegal, "e1d2": Unknown,
		}},
		{"double check", "4k3/4r3/8/8/1N6/3n4/8/R3K3 w - - 0 1", map[string]Verdict{
			"b4d3": Illegal, "a1d1": Illegal, "a1a2": Illegal, "e1f2": Unknown,
		}},
		{"en passant out of check", "8/8/8/2k5/3Pp3/8/8/4K3 b - d3 0 1", map[string]Verdict{
			"e4d3": Unknown, "c5d4": Unknown, "e4e3": Illegal,
		}},
		{"not in check", "4r2k/8/8/8/8/8/4R3/4K1N1 w - - 0 1", map[string]Verdict{
			"g1f3": Legal, "e2d2": Unknown, "e1d1": Unknown,
		}},
	}
	for _, tc := range cases {
		g, err := ParseFEN(tc.fen)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		inCheck := moves.IsInCheck(&g.Board, g.Turn)
		pseudo, legality := g.AppendPseudoLegalMoves(nil, g.Turn, inCheck)
		index := map[string]Move{}
		for _, m := range pseudo {
			index[m.UCI()] = m
		}
		for uci, want := range tc.want {
			m, ok := index[uci]
			if !ok {
				t.Fatalf("%s: %s is not in the pseudo-legal list", tc.name, uci)
			}
			if got := legality.Screen(&g.Board, m); got != want {
				t.Errorf("%s: Screen(%s) = %v, want %v", tc.name, uci, got, want)
			}
		}
	}
}

// The three ways a pseudo-legal move is illegal, one position each: a
// pinned piece leaving its line, a move that ignores a check, and the en
// passant capture that opens a rank to the king. Each must be generated,
// flagged for the test, and rejected by it.
func TestPseudoLegalMovesKeepTheIllegalOnesForLater(t *testing.T) {
	cases := []struct {
		name, fen, illegal, legal string
	}{
		{"pinned rook", "4r2k/8/8/8/8/8/4R3/4K3 w - - 0 1", "e2d2", "e2e8"},
		{"check ignored", "4k3/4r3/8/8/8/8/8/R3K3 w - - 0 1", "a1a2", "e1d1"},
		{"en passant pin", "8/8/8/KPp4r/8/8/8/7k w - c6 0 1", "b5c6", "b5b6"},
	}
	for _, tc := range cases {
		g, err := ParseFEN(tc.fen)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		inCheck := moves.IsInCheck(&g.Board, g.Turn)
		pseudo, legality := g.AppendPseudoLegalMoves(nil, g.Turn, inCheck)
		index := map[string]Move{}
		for _, m := range pseudo {
			index[m.UCI()] = m
		}
		bad, ok := index[tc.illegal]
		if !ok {
			t.Fatalf("%s: %s is not in the pseudo-legal list", tc.name, tc.illegal)
		}
		if !legality.NeedsTest(&g.Board, bad) {
			t.Errorf("%s: %s is not flagged for the legality test", tc.name, tc.illegal)
		}
		if legality.IsLegal(&g.Board, bad) {
			t.Errorf("%s: %s reported legal", tc.name, tc.illegal)
		}
		good, ok := index[tc.legal]
		if !ok {
			t.Fatalf("%s: %s is not in the pseudo-legal list", tc.name, tc.legal)
		}
		if !legality.IsLegal(&g.Board, good) {
			t.Errorf("%s: %s reported illegal", tc.name, tc.legal)
		}
		for _, m := range g.AppendLegalMoves(nil, g.Turn) {
			if m.UCI() == tc.illegal {
				t.Errorf("%s: AppendLegalMoves still returns %s", tc.name, tc.illegal)
			}
		}
	}
}

// The zero Legality is for a list that is legal already, such as
// AppendLegalMoves returns: it tests nothing and accepts everything, even
// in a position where the same moves would need the test.
func TestZeroLegalityTestsNothing(t *testing.T) {
	g, err := ParseFEN("4k3/4r3/8/8/8/8/8/R3K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	var zero Legality
	for _, m := range g.AppendLegalMoves(nil, g.Turn) {
		if zero.NeedsTest(&g.Board, m) || !zero.IsLegal(&g.Board, m) {
			t.Errorf("zero Legality tested %s", m.UCI())
		}
	}
}

// Appending keeps what the buffer already holds, as every Append* here does.
func TestPseudoLegalMovesAppendAfterTheBuffer(t *testing.T) {
	g := New()
	sentinel := Move{From: board.Sq{File: 7, Rank: 7}, To: board.Sq{File: 7, Rank: 7}}
	got, _ := g.AppendPseudoLegalMoves([]Move{sentinel}, board.White, false)
	if len(got) != 21 || got[0] != sentinel {
		t.Fatalf("got %d moves starting %v, want the sentinel then 20", len(got), got[0])
	}
}
