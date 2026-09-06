package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// A search must leave the position it was given exactly as it found it.
//
// Null-move pruning did not. It handed the move to the opponent without
// clearing the en passant square, so the opponent's generator produced an
// en passant capture onto a square nobody had double-pushed to; that
// removes a pawn which is not there, and the unmake then places a phantom
// pawn on the board. After 1.e4, scoring the position at depth 3 returned
// a board with a black pawn on e2.
//
// The blast radius was much wider than the null-move subtree. The search
// works on the caller's board, so every caller was affected: the PGN
// importer saw it as 47% of games "failing to replay", and self-play
// generation saw nothing at all, because a corrupted position does not
// announce itself. It simply searched and labelled a position that had
// never occurred.
func TestSearchDoesNotMutateTheGivenPosition(t *testing.T) {
	positions := []string{
		// En passant available, which is the trigger.
		"rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1",
		"rnbqkbnr/ppp1pppp/8/3p4/4P3/8/PPPP1PPP/RNBQKBNR w KQkq d6 0 2",
		"rnbqkbnr/pppp1ppp/8/4p3/3P4/8/PPP1PPPP/RNBQKBNR w KQkq e6 0 2",
		// And without, as a control.
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
	}
	for _, fen := range positions {
		for _, depth := range []int{1, 2, 3, 4, 5} {
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			before := g.FEN()
			PlayerScoreWith(Strong(depth), g, nil)
			if after := g.FEN(); after != before {
				t.Errorf("depth %d changed the position\n  before %s\n  after  %s",
					depth, before, after)
			}

			g2, _ := game.ParseFEN(fen)
			before2 := g2.FEN()
			PlayerPick(Strong(depth), g2)
			if after := g2.FEN(); after != before2 {
				t.Errorf("depth %d: picking a move changed the position\n  before %s\n  after  %s",
					depth, before2, after)
			}
		}
	}
}

// The null move itself must not leave an en passant right behind for the
// opponent, which is what made the phantom pawn possible.
func TestNullMoveClearsEnPassant(t *testing.T) {
	g, err := game.ParseFEN("rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1")
	if err != nil {
		t.Fatal(err)
	}
	if sq, ok := g.Board.EPSquare(); !ok || sq.File != 4 || sq.Rank != 2 {
		t.Fatalf("fixture lost its en passant square: %v %v", sq, ok)
	}
	before := g.FEN()
	p := Strong(4)
	p.NullMove = true
	PlayerScoreWith(p, g, nil)
	// The right must survive the search unchanged, having been restored.
	if sq, ok := g.Board.EPSquare(); !ok || sq != (board.Sq{File: 4, Rank: 2}) {
		t.Errorf("en passant square is %v %v after the search, want e3 restored", sq, ok)
	}
	if after := g.FEN(); after != before {
		t.Errorf("position changed:\n  before %s\n  after  %s", before, after)
	}
}
