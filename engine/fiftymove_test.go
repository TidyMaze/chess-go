package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// A position where the fifty move clock has run out is a draw, whatever
// the material says. The search never knew: IsFiftyMoveDraw existed and
// only the training loop called it, so a dead drawn position was scored
// as won and the engine had no reason to avoid walking into it.
func TestFiftyMoveDrawScoresAsADraw(t *testing.T) {
	// Rook and three pawns against rook and one, the ending from a real
	// bullet game that was drawn this way, with the clock already spent.
	g, err := game.ParseFEN("6k1/5p2/4rP2/6PR/1K5P/8/8/8 w - - 100 51")
	if err != nil {
		t.Fatal(err)
	}
	got := evalPosition(g, board.White, &Eval{})
	if got != 0 {
		t.Errorf("a position at 100 halfmoves scored %+.2f, want 0: it is a draw", got)
	}
}

// Short of the limit the score must fade toward the draw as the clock
// runs, so a line that resets it (a pawn move or a capture) is worth
// something and shuffling is not. Without this the engine shuffled a
// rook for fifty moves two pawns up and drew: the horizon is far past
// any search depth at clock 41, so a terminal check alone cannot see it.
func TestScoreFadesAsTheFiftyMoveClockRuns(t *testing.T) {
	fresh, err := game.ParseFEN("6k1/5p2/4rP2/6PR/1K5P/8/8/8 w - - 0 51")
	if err != nil {
		t.Fatal(err)
	}
	late, err := game.ParseFEN("6k1/5p2/4rP2/6PR/1K5P/8/8/8 w - - 90 51")
	if err != nil {
		t.Fatal(err)
	}
	ev := &Eval{}
	a := evalPosition(fresh, board.White, ev)
	b := evalPosition(late, board.White, ev)
	if a <= 0 {
		t.Fatalf("expected white to be better here, got %+.2f", a)
	}
	if b >= a {
		t.Errorf("score at clock 90 is %+.2f, not less than %+.2f at clock 0: nothing pushes the engine to make progress", b, a)
	}
}

// An ordinary position with a fresh clock must be untouched, so this
// cannot quietly change how the engine plays everywhere else.
func TestFreshClockLeavesTheScoreAlone(t *testing.T) {
	g, err := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	if err != nil {
		t.Fatal(err)
	}
	ev := &Eval{}
	withClock := evalPosition(g, board.White, ev)
	plain := PositionScoreEval(&g.Board, board.White, ev)
	if withClock != plain {
		t.Errorf("a fresh clock changed the score: %+.4f against %+.4f", withClock, plain)
	}
}
