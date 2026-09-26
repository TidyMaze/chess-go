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

// The table's key knows nothing of the clock, so a mate it stored at a low
// clock can come back at a high one. Twelve plies from the node, the mate
// still stands at clock 88: the mating move is the hundredth halfmove, and
// FIDE 9.3 lets it count. At clock 89 the rule strikes first unless a
// capture or a pawn move comes along, and the table cannot tell which, so
// it must not cut off on it, for either side.
func TestTableForgetsAMateTheClockCannotReach(t *testing.T) {
	tt := NewTranspositionTable(10)
	const win, loss = 0x0123456789abcdef, 0x0fedcba987654321
	tt.store(win, mateScore-12, 5, 0, ttExact, board.White)
	tt.store(loss, -(mateScore - 12), 5, 0, ttExact, board.White)
	if got, ok := tt.probe(win, 5, 0, 88, board.White, negInf, posInf); !ok || got != mateScore-12 {
		t.Errorf("mate in 12 plies at clock 88: %v (ok %v), want %v", got, ok, mateScore-12)
	}
	for _, key := range []uint64{win, loss} {
		if got, ok := tt.probe(key, 5, 0, 89, board.White, negInf, posInf); ok {
			t.Errorf("probe at clock 89 cut off on %v, a mate 12 plies away that the fifty-move rule reaches first", got)
		}
		if got, cutoff, _, okMove := tt.probeWithMove(key, 5, 0, 89, board.White, negInf, posInf); cutoff || !okMove {
			t.Errorf("probeWithMove at clock 89: cutoff %v on %v (move %v), want no cutoff and the move", cutoff, got, okMove)
		}
	}
}

// Close to the limit a stored score of any kind may hide the draw: it was
// searched at a clock where the rule was far away. Stockfish stops taking
// table cutoffs at clock 90 for the same reason; the move is still used.
func TestTableGivesNoCutoffNearTheFiftyMoveLimit(t *testing.T) {
	tt := NewTranspositionTable(10)
	const key = 0x1111222233334444
	m := game.Move{From: board.Sq{File: 4, Rank: 1}, To: board.Sq{File: 4, Rank: 3}}
	tt.storeWithMove(key, 4, 5, 0, ttExact, board.White, m)
	if got, cutoff, _, _ := tt.probeWithMove(key, 5, 0, 89, board.White, negInf, posInf); !cutoff || got != 4 {
		t.Errorf("clock 89: cutoff %v on %v, want a cutoff on 4", cutoff, got)
	}
	if got, cutoff, move, okMove := tt.probeWithMove(key, 5, 0, 90, board.White, negInf, posInf); cutoff || !okMove || move != m {
		t.Errorf("clock 90: cutoff %v on %v, move %v (ok %v), want no cutoff and %v", cutoff, got, move, okMove, m)
	}
	if got, ok := tt.probe(key, 5, 0, 90, board.White, negInf, posInf); ok {
		t.Errorf("probe at clock 90 cut off on %v", got)
	}
}

// A node right after a capture or a pawn move has clock 0, whatever the
// root's clock. The evaluation read 0 as "not set" and fell back to the
// game's own clock, which inside the search is the root's, so a leaf right
// after d6 was faded as if nothing had been reset, and the fade stopped
// rewarding progress at the horizon. Review measured d5d6 at depth 1
// scoring 10.85, 8.14 and 5.49 at root clocks 0, 60 and 99.
func TestNodeAfterAResetFadesAtItsOwnClock(t *testing.T) {
	var scores []float64
	for _, rootClock := range []string{"0", "60", "99"} {
		// The Lichess position after d6, the game's clock left at the root's.
		g := mustFEN(t, "8/6B1/1k1PKRp1/6P1/1p6/1P6/1P6/8 b - - "+rootClock+" 105")
		ctx := searchCtxPool.Get().(*searchCtx)
		ctx.reset()
		ctx.ev, ctx.quiescence, ctx.extensions = &Eval{}, false, false
		ctx.fifty[1] = 0
		scores = append(scores, ctx.search(g, board.Black, board.White, 0, 1, negInf, posInf))
		searchCtxPool.Put(ctx)
	}
	if scores[1] != scores[0] || scores[2] != scores[0] {
		t.Errorf("the leaf after d6 (clock 0) scored %v at root clocks 0, 60, 99; it must not depend on the root's clock", scores)
	}
}

// The Lichess bot keeps one table for the whole game. Searched first at a
// fresh clock, the table holds a mate in two behind Kg6; at clock 98 that
// line runs into the rule (Kg6 Kg8 is the hundredth halfmove, before Ra8
// mates), and only c3 or c4 keeps the win. Found by review: with the warm
// table the engine played Kg6 or Kf7 at every depth, scored as mate.
func TestTableFromAFreshClockDoesNotHideTheFiftyMoveDraw(t *testing.T) {
	for _, depth := range []int{4, 6, 8} {
		p := botAtDepth(t, depth)
		tt := NewTranspositionTable(18)
		SeedRandom(1)
		p.ChooseMoveScored(mustFEN(t, "7k/8/5K2/8/8/8/2P5/R7 w - - 0 100"), tt)
		SeedRandom(1)
		m, score, _ := p.ChooseMoveScored(mustFEN(t, "7k/8/5K2/8/8/8/2P5/R7 w - - 98 100"), tt)
		if got := m.UCI(); got != "c2c3" && got != "c2c4" {
			t.Errorf("depth %d: played %s (score %v) at clock 98 with the table from clock 0; only c3 and c4 avoid the draw", depth, got, score)
		}
	}
}
