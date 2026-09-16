package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// A quiet check is the tactic a captures-only quiescence never sees: here
// Nc7+ forks king and rook, and the rook falls next move. With QChecks the
// quiescence root looks at the check and finds the rook; without it the
// position stands pat a knight against a rook. The white pawn keeps the
// winning side off the dead-draw rule, which scored KN v K as zero.
func TestQuiescenceWithChecksFindsTheFork(t *testing.T) {
	g, err := game.ParseFEN("r3k3/8/8/3N4/8/8/7P/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	score := func(qchecks bool) float64 {
		ev := &Eval{UsePST: true, QuiescePly: 8, QChecks: qchecks}
		ev.Table = NewTranspositionTable(12)
		ctx := searchCtxPool.Get().(*searchCtx)
		defer searchCtxPool.Put(ctx)
		ctx.reset()
		ev.acc = &ctx.acc
		return quiesceWithKey(g, zobristBoard(&g.Board, board.White), board.White, board.White, negInf, posInf, ev, 0, 0)
	}
	off, on := score(false), score(true)
	t.Logf("quiescence score: %.2f without checks, %.2f with", off, on)
	if on-off < 2 {
		t.Errorf("checks in quiescence should find the rook: %.2f without, %.2f with", off, on)
	}
}

// Only the first quiescence ply generates checks; deeper it is captures
// again, or the tree explodes. This pins the flag plumbing end to end.
func TestQChecksIsAFeatureFlag(t *testing.T) {
	var p Player
	p.ApplyFeatures("qchecks")
	if !p.QChecks {
		t.Fatal("qchecks did not switch on")
	}
	if !evalForPlayer(p).QChecks {
		t.Fatal("QChecks did not reach the Eval")
	}
}
