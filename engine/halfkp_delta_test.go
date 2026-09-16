package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// A king crossing into another bucket changes every feature of its own
// perspective and none of the other side's, so only that perspective is
// rebuilt in full; the other is still one copy plus the rows that moved.
// Before this, refresh decided incremental-or-full for the node as a
// whole and a king move diffed thirty rows out and thirty back in.
func TestOnlyTheMovedKingsPerspectiveIsRebuilt(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network here:", err)
	}
	g, err := game.ParseFEN("4k3/pppppppp/8/8/8/8/PPPPPPPP/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	var stack [2]halfKPAcc
	var stats halfKPAccStats
	net.EvaluateWith(&g.Board, &stack[0], nil, &stats)

	// e1 to f1 changes the mirrored file, so the king slot changes under
	// both the 8-bucket and the 32-square scheme.
	g.Board.MakeMove(board.Sq{File: 4, Rank: 0}, board.Sq{File: 5, Rank: 0})
	stats = halfKPAccStats{}
	net.EvaluateWith(&g.Board, &stack[1], &stack[0], &stats)
	if stats.full != 1 || stats.incremental != 1 {
		t.Errorf("after a white king move: %d full rebuilds and %d incremental updates, want 1 and 1 (white rebuilt, black incremental)",
			stats.full, stats.incremental)
	}
}

// A quiet move by any other piece touches two features of each
// perspective, so both sides are incremental.
func TestAQuietMoveKeepsBothPerspectivesIncremental(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network here:", err)
	}
	g := game.New()
	var stack [2]halfKPAcc
	var stats halfKPAccStats
	net.EvaluateWith(&g.Board, &stack[0], nil, &stats)
	g.Board.MakeMove(board.Sq{File: 4, Rank: 1}, board.Sq{File: 4, Rank: 3})
	stats = halfKPAccStats{}
	net.EvaluateWith(&g.Board, &stack[1], &stack[0], &stats)
	if stats.full != 0 || stats.incremental != 2 {
		t.Errorf("after e2e4: %d full rebuilds and %d incremental updates, want 0 and 2", stats.full, stats.incremental)
	}
}
