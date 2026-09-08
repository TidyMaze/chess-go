package engine

import (
	"math"
	"testing"
	"time"

	"chess/board"
	"chess/game"
)

// Bugs found by reading the search with the question "which moves does
// each pruning rule think this is?", each proven red here before the fix.

func mustFEN(t *testing.T, fen string) *game.Game {
	t.Helper()
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func plainEval(see bool) *Eval {
	return &Eval{UsePST: true, Tapered: true, Structure: true, Mobility: true,
		KingSafety: 0.01, SEEPruning: see, QuiescePly: 6}
}

// 1. Quiescence must see an en passant capture. It tested "is there a
// piece on the destination square", and for en passant there is not.
func TestQuiescenceSeesEnPassant(t *testing.T) {
	g := mustFEN(t, "4k3/8/8/3pP3/8/8/8/4K3 w - d6 0 2")
	ev := plainEval(false)
	static := evalPosition(g, board.White, ev)
	q := quiesce(g, board.White, board.White, negInf, posInf, ev, 0)
	if q < static+0.5 {
		t.Errorf("exd6 wins a pawn en passant, but quiescence returned %.3f against a "+
			"static %.3f: it never saw the capture", q, static)
	}
}

// 2. Quiescence must recognise checkmate and stalemate. It stood pat in
// any position, including one where the side to move has no legal move.
func TestQuiescenceRecognisesMateAndStalemate(t *testing.T) {
	const mateBound = mateScore - maxSearchPly
	// Qxf7# is a capture, so quiescence reaches the mated position itself.
	g := mustFEN(t, "r1bqkb1r/pppp1ppp/2n2n2/4p2Q/2B1P3/8/PPPP1PPP/RNB1K1NR w KQkq - 4 4")
	if q := quiesce(g, board.White, board.White, negInf, posInf, plainEval(false), 0); q < mateBound {
		t.Errorf("Qxf7 is mate and quiescence scored it %.3f", q)
	}
	// Black to move, stalemated: the score is 0, not the material count.
	s := mustFEN(t, "7k/5Q2/6K1/8/8/8/8/8 b - - 0 1")
	if q := quiesce(s, board.Black, board.White, negInf, posInf, plainEval(false), 0); math.Abs(q) > 1e-9 {
		t.Errorf("stalemate scored %.3f from quiescence, want 0", q)
	}
}

// 3. A capture that promotes must promote inside quiescence too. The
// board layer does not know the rule and quiescence never applied it, so
// exd8 left a pawn standing on d8.
func TestQuiescencePromotesOnCapture(t *testing.T) {
	g := mustFEN(t, "3r3k/4P3/8/8/8/8/8/K7 w - - 0 1")
	ev := plainEval(false)
	static := evalPosition(g, board.White, ev)
	q := quiesce(g, board.White, board.White, negInf, posInf, ev, 0)
	// Pawn for rook is about -4; queen against nothing about +9.
	if q < static+10 {
		t.Errorf("exd8 wins a rook and makes a queen: quiescence %.3f, static %.3f, "+
			"so the pawn stayed a pawn", q, static)
	}
}

// 4. A quiet promotion is not a quiet move. Quiescence considered only
// captures, so a pawn one step from queening was invisible to it.
func TestQuiescenceConsidersQuietPromotion(t *testing.T) {
	g := mustFEN(t, "7k/4P3/8/8/8/8/8/K7 w - - 0 1")
	ev := plainEval(false)
	static := evalPosition(g, board.White, ev)
	q := quiesce(g, board.White, board.White, negInf, posInf, ev, 0)
	if q < static+6 {
		t.Errorf("e8=Q is available: quiescence %.3f, static %.3f", q, static)
	}
}

// 5. Static exchange pruning must never prune a capture that gives check.
// It pruned Qxf7# as "queen takes defended pawn".
func TestSEEPruningKeepsCheckingCaptures(t *testing.T) {
	const mateBound = mateScore - maxSearchPly
	g := mustFEN(t, "r1bqkb1r/pppp1ppp/2n2n2/4p2Q/2B1P3/8/PPPP1PPP/RNB1K1NR w KQkq - 4 4")
	if q := quiesce(g, board.White, board.White, negInf, posInf, plainEval(true), 0); q < mateBound {
		t.Errorf("with SEE pruning on, Qxf7# scored %.3f: the mating capture was pruned", q)
	}
}

// 6. The evaluation must be told the real side to move. It read g.Turn,
// which the search never updates, so every node at an odd ply reported
// the root's side. Only the tablebase probe reads it today, and the probe
// indexed the wrong colour on half of all nodes.
func TestEvaluationKnowsTheRealSideToMove(t *testing.T) {
	g := mustFEN(t, "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	// One ply into the tree the way the search does it: board only.
	undo := g.Board.MakeMove(board.Sq{File: 4, Rank: 1}, board.Sq{File: 4, Rank: 3})
	defer g.Board.UnmakeMove(undo)
	ev := plainEval(false)
	evalPositionFor(g, board.Black, board.White, ev)
	if ev.STM != board.Black {
		t.Errorf("Black is to move one ply in, the evaluation was told %v", ev.STM)
	}
}

// 7. Insufficient material is a draw. King and bishop against king was
// worth three pawns to the search, so it steered into dead positions and
// away from trades that reach them.
func TestInsufficientMaterialIsDrawn(t *testing.T) {
	for _, fen := range []string{
		"8/8/8/8/8/8/8/KB5k w - - 0 1",
		"8/8/8/8/8/8/8/KN5k b - - 0 1",
		"8/8/8/8/8/8/8/K6k w - - 0 1",
		"8/8/8/8/8/8/8/KB1b3k w - - 0 1", // both bishops on light squares
	} {
		g := mustFEN(t, fen)
		s, ok := PlayerScoreWith(Strong(3), g, nil)
		if !ok {
			t.Fatalf("%s: no moves", fen)
		}
		if math.Abs(s) > 1e-9 {
			t.Errorf("%s scores %.3f, but nobody can ever mate here", fen, s)
		}
	}
	// And not over-applied: a lone pawn can still win.
	g := mustFEN(t, "8/8/8/8/8/8/P7/K6k w - - 0 1")
	if s, _ := PlayerScoreWith(Strong(3), g, nil); math.Abs(s) < 0.5 {
		t.Errorf("king and pawn against king scored %.3f, that is not dead", s)
	}
}

// 8. A search that runs out of time must not leave its half-finished
// scores in the shared table. Aborted children return 0, their parents
// treated that as a value and stored it, and the table is reused for
// the rest of the game.
func TestAbortedSearchDoesNotPoisonTheTable(t *testing.T) {
	g := mustFEN(t, "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1")
	p := exactPlayer(5)
	clean, _ := PlayerScoreWith(p, g, nil)

	table := NewTranspositionTable(16)
	ev := evalForPlayer(p)
	ev.Table = table
	ctx := &searchCtx{}
	ctx.reset()
	ctx.ev, ctx.quiescence, ctx.extensions = ev, false, false
	ctx.path[0] = zobristBoard(&g.Board, g.Turn)
	ctx.played = playedKeys(g)
	// The clock is read every 2048 nodes, so starting the count at 1 with
	// a deadline already passed aborts deterministically at node 2048,
	// deep inside the tree, with plenty of parents left to unwind.
	ctx.nodes = 1
	ctx.deadline = time.Now().Add(-time.Second)
	ctx.search(g, g.Turn, g.Turn, 5, 0, negInf, posInf)
	if !ctx.aborted {
		t.Fatal("the search did not abort, the test proves nothing")
	}

	reused, _ := PlayerScoreWith(p, g, table)
	if math.Abs(reused-clean) > 1e-9 {
		t.Errorf("after an aborted search the reused table gives %.6f where a clean "+
			"table gives %.6f: the abort left wrong entries behind", reused, clean)
	}
}

// 9. The main search must classify en passant as a capture too, or
// futility pruning skips it and reductions shrink it.
func TestSearchClassifiesEnPassantAsCapture(t *testing.T) {
	g := mustFEN(t, "4k3/8/8/3pP3/8/8/8/4K3 w - d6 0 2")
	m := game.Move{From: board.Sq{File: 4, Rank: 4}, To: board.Sq{File: 3, Rank: 5}}
	if !isCaptureMove(g, m) {
		t.Error("exd6 en passant is a capture and the search says it is quiet")
	}
}

// 10. The game history must be keyed on the side that was to move in each
// past position, or in-search repetition detection against the game never
// fires. Nf3 Nf6 Ng1 Ng8 returns to the start: that position has now
// occurred twice, both times with White to move.
func TestPlayedKeysCountOccurrencesWithTheRightSideToMove(t *testing.T) {
	g := game.New()
	g.TrackRepetition = true
	sq := func(f, r int) board.Sq { return board.Sq{File: f, Rank: r} }
	for _, mv := range [][2]board.Sq{
		{sq(6, 0), sq(5, 2)}, {sq(6, 7), sq(5, 5)}, {sq(5, 2), sq(6, 0)}, {sq(5, 5), sq(6, 7)},
	} {
		g.ApplyMove(mv[0], mv[1])
	}
	start := game.New()
	keys := playedKeys(g)
	if n := keys[zobristBoard(&start.Board, board.White)]; n != 2 {
		t.Errorf("start position, White to move, occurred twice; history counts %d", n)
	}
	if n := keys[zobristBoard(&start.Board, board.Black)]; n != 0 {
		t.Errorf("start position never occurred with Black to move; history counts %d", n)
	}
}
