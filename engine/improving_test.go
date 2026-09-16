package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// A node is "improving" when the side to move stands better than it did
// two plies ago, by static evaluation. Late move pruning then prunes less
// ((3 + depth^2) / (2 - improving) moves survive), because a rising
// position is one where quiet moves are doing work. The LMP call had this
// hardwired to false since the day it was written.
func TestImprovingComparesWithTwoPliesAgo(t *testing.T) {
	ctx := searchCtxPool.Get().(*searchCtx)
	defer searchCtxPool.Put(ctx)
	ctx.reset()
	ctx.ev = &Eval{Improving: true}

	// Maximiser at ply 2, static 0.3 here against 0.1 two plies ago.
	ctx.noteStatic(0, 0.1)
	if !ctx.improving(2, 0.3, true, true) {
		t.Error("maximiser rose from 0.1 to 0.3 and should be improving")
	}
	if ctx.improving(2, 0.0, true, true) {
		t.Error("maximiser fell from 0.1 to 0.0 and is not improving")
	}
	// Minimiser improves when the score falls.
	if !ctx.improving(2, -0.2, true, false) {
		t.Error("minimiser went from 0.1 to -0.2 and should be improving")
	}
	// Unknown static at this node, or at ply-2, or the root: never improving.
	if ctx.improving(2, 0.9, false, true) {
		t.Error("no static evaluation here means no claim")
	}
	if ctx.improving(1, 0.9, true, true) || ctx.improving(0, 0.9, true, true) {
		t.Error("nothing two plies back at ply 0 or 1")
	}
	if ctx.improving(4, 0.9, true, true) {
		t.Error("ply 2 was never noted, so ply 4 cannot compare")
	}
	ctx.ev.Improving = false
	if ctx.improving(2, 0.3, true, true) {
		t.Error("with the feature off the answer is the old constant, false")
	}
}

// Switching the feature on must change the tree: the LMP threshold moves
// at improving nodes. Off, the node counts are the pinned ones.
func TestImprovingChangesTheSearchTreeOnlyWhenOn(t *testing.T) {
	var with, without int
	for _, fen := range correctnessPositions[:4] {
		g, _ := game.ParseFEN(fen)
		off, on := Strong(6), Strong(6)
		off.LMP, on.LMP = true, true
		off.Futility, on.Futility = true, true
		on.Improving = true
		ResetNodes()
		PlayerScoreWith(off, g, nil)
		without += TotalNodes()
		ResetNodes()
		PlayerScoreWith(on, g, nil)
		with += TotalNodes()
	}
	t.Logf("nodes at depth 6: %d without improving, %d with", without, with)
	if with == without {
		t.Error("the flag changed nothing, so improving never reached late move pruning")
	}
}

func TestImprovingIsAFeatureFlag(t *testing.T) {
	var p Player
	p.ApplyFeatures("improving")
	if !p.Improving || !evalForPlayer(p).Improving {
		t.Fatal("improving did not reach the Eval")
	}
	_ = board.White
}
