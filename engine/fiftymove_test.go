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

// The rule saves games as well as costing them. Of seven drawn games,
// two were wins thrown away at a spent clock, and one was a draw rescued
// from six pawns down at a spent clock. So a losing side must see the
// clock running as good news, exactly as the winning side sees it as bad:
// the fade has to work on a negative score too, or the engine would walk
// away from its own drawing resource.
func TestFiftyMoveFadeHelpsTheLosingSideToo(t *testing.T) {
	// Six pawns down, the material from the game that was saved this way.
	losing := -6.0
	fresh := fadeForFiftyMove(losing, 0)
	late := fadeForFiftyMove(losing, 90)
	if late <= fresh {
		t.Errorf("a losing score reads %+.2f at a spent clock against %+.2f at a fresh one; the draw must look better as the clock runs", late, fresh)
	}
	if drawn := fadeForFiftyMove(losing, 100); drawn != 0 {
		t.Errorf("a spent clock scored %+.2f, want the draw", drawn)
	}
}

// Lichess lhT8MqvU, the last move: White a rook, a bishop and two pawns up
// at clock 99, and every move except d6 and Rxg6 draws on the spot. The
// game played Bf8 and drew. The fade only reached a node at the horizon,
// so a node past clock 100 kept being searched, and a pawn push or capture
// found below it brought the won score back: a line that was already a
// draw scored as a win. Before the fix, up to depth 11 the engine took on
// g6 for the material; from depth 12 it saw a mate in six that starts with
// a quiet move, scored it 989 and played Bf8, Ke5 or Kd6, all of them draws.
func TestFiftyMoveClockAt99MustBeReset(t *testing.T) {
	p := botAtDepth(t, 12)
	g := mustFEN(t, "8/6B1/1k2KRp1/3P2P1/1p6/1P6/1P6/8 w - - 99 105")
	SeedRandom(1)
	m, score, ok := PlayerPickScored(p, g)
	if !ok {
		t.Fatal("no move")
	}
	if got := m.UCI(); got != "d5d6" && got != "f6g6" {
		t.Errorf("played %s (score %v) at clock 99; only d5d6 and f6g6 reset the clock, anything else is a draw", got, score)
	}
}

// A null move passes the turn without playing a move, so its child keeps
// the parent's clock. It used to read whatever an earlier sibling had left
// in c.fifty[ply+1]; left at 100, passing looked like an instant draw and
// the losing side cut off on it.
func TestNullMoveChildKeepsTheParentsClock(t *testing.T) {
	// Black to move, a queen down, fresh clock: worth about +9 to White.
	g := mustFEN(t, "4k3/8/8/8/8/8/8/Q3K3 b - - 0 1")
	ctx := searchCtxPool.Get().(*searchCtx)
	defer searchCtxPool.Put(ctx)
	ctx.reset()
	ctx.ev = &Eval{NullMove: true}
	ctx.fifty[1] = 0
	ctx.fifty[2] = 100 // left behind by an earlier sibling
	if got := ctx.search(g, board.Black, board.White, 3, 1, 1, 2); got < 2 {
		t.Errorf("a queen up scored %v against the window (1, 2); the null move read a stale clock as a draw", got)
	}
}

// FIDE 9.3: when the move that completes fifty moves gives checkmate, the
// mate stands. Ra8 is a rook move at clock 99, so it reaches 100, and it
// is mate: the search must score it as mate in one, not as a draw.
func TestMateOnTheHundredthHalfmoveIsStillMate(t *testing.T) {
	p := botAtDepth(t, 4)
	g := mustFEN(t, "6k1/5ppp/8/8/8/8/5PPP/R5K1 w - - 99 60")
	SeedRandom(1)
	m, score, ok := PlayerPickScored(p, g)
	if !ok || m.UCI() != "a1a8" || score != mateScore-1 {
		t.Errorf("played %s scoring %v (ok %v), want a1a8 scoring %v", m.UCI(), score, ok, float64(mateScore-1))
	}
}
