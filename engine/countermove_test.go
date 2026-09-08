package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

func mv(f1, r1, f2, r2 int) game.Move {
	return game.Move{From: board.Sq{File: f1, Rank: r1}, To: board.Sq{File: f2, Rank: r2}}
}

// A quiet move that refuted the previous move is remembered against that
// previous move, and ordered right after the killers next time the same
// previous move is seen, regardless of position.
func TestCountermoveIsRecordedAndOrderedAfterKillers(t *testing.T) {
	c := &searchCtx{}
	c.reset()
	prev := mv(4, 1, 4, 3)  // e2e4
	reply := mv(2, 6, 2, 4) // c7c5
	c.recordCounter(board.Black, prev, reply)
	if got := c.counterFor(board.Black, prev); got != reply {
		t.Fatalf("countermove for e2e4: %v, want c7c5", got)
	}
	if got := c.counterFor(board.Black, mv(3, 1, 3, 3)); got != (game.Move{}) {
		t.Errorf("a previous move never seen has a countermove: %v", got)
	}
	// Ordering: killer first, then the countermove, then a plain quiet move.
	g, _ := game.ParseFEN("rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1")
	killer := mv(4, 6, 4, 4) // e7e5
	c.recordKiller(1, killer)
	c.prevMove = prev
	c.ev = &Eval{Countermoves: true}
	ms := []game.Move{mv(0, 6, 0, 5), reply, killer, mv(7, 6, 7, 5)}
	c.orderMoves(g, ms, game.Move{}, 1, board.Black)
	if ms[0] != killer || ms[1] != reply {
		t.Errorf("order %v: want the killer first and the countermove second", ms)
	}
	// Switched off, the countermove is just another quiet move.
	c.ev = &Eval{}
	ms2 := []game.Move{mv(0, 6, 0, 5), reply, killer}
	c.orderMoves(g, ms2, game.Move{}, 1, board.Black)
	if ms2[1] == reply && ms2[0] == killer {
		// Fine only if history ordered it there by chance: history is empty,
		// so equal scores keep input order, and reply came before a7a6? No:
		// input order is a7a6, reply, killer -> stable sort keeps a7a6 first.
		t.Errorf("with the heuristic off the countermove was still promoted: %v", ms2)
	}
}

// Wired in, the heuristic must remove nodes: better ordering means
// earlier cutoffs.
func TestCountermovesCutNodes(t *testing.T) {
	var with, without int
	for _, fen := range correctnessPositions[:6] {
		g, _ := game.ParseFEN(fen)
		off, on := Strong(6), Strong(6)
		on.Countermoves = true
		ResetNodes()
		PlayerScoreWith(off, g, nil)
		without += TotalNodes()
		ResetNodes()
		PlayerScoreWith(on, g, nil)
		with += TotalNodes()
	}
	t.Logf("nodes: %d without countermoves, %d with (%.2fx)", without, with, float64(without)/float64(with))
	if with >= without {
		t.Errorf("countermoves removed no nodes: %d with, %d without", with, without)
	}
}
