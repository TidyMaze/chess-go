package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// History with gravity stays bounded and keeps discriminating. The plain
// running sum does neither: it grows without limit, so every move that has
// ever cut off saturates into the same large number and the table stops
// telling moves apart. The standard form scales the update by how far the
// entry already is from the ceiling.
func TestHistoryWithGravityStaysBounded(t *testing.T) {
	g := game.New()
	ctx := searchCtxPool.Get().(*searchCtx)
	defer searchCtxPool.Put(ctx)
	ctx.reset()
	ctx.ev = &Eval{HistGravity: true}

	m := game.Move{From: board.Sq{File: 6, Rank: 0}, To: board.Sq{File: 5, Rank: 2}}
	for i := 0; i < 2000; i++ {
		ctx.recordHistory(board.White, g, m, 12)
	}
	v := ctx.history[board.White][sqIndex(m.From)][sqIndex(m.To)]
	if v > maxHistory {
		t.Errorf("history reached %d, past the %d ceiling", v, maxHistory)
	}
	if v < maxHistory/2 {
		t.Errorf("history only reached %d after 2000 cutoffs at depth 12; it should approach the ceiling", v)
	}

	// A move punished as often as it is rewarded must not sit near the top.
	other := game.Move{From: board.Sq{File: 1, Rank: 0}, To: board.Sq{File: 2, Rank: 2}}
	for i := 0; i < 2000; i++ {
		ctx.recordHistory(board.White, g, other, 12)
		ctx.penalizeHistory(board.White, g, []game.Move{other}, 12)
	}
	if got := ctx.history[board.White][sqIndex(other.From)][sqIndex(other.To)]; got >= v {
		t.Errorf("a move rewarded and punished equally scores %d against %d for one only rewarded", got, v)
	}

	// Without the flag the old running sum is unchanged, so node counts are.
	ctx.reset()
	ctx.ev = &Eval{}
	ctx.recordHistory(board.White, g, m, 5)
	if got := ctx.history[board.White][sqIndex(m.From)][sqIndex(m.To)]; got != 25 {
		t.Errorf("with the flag off the update is depth*depth = 25, got %d", got)
	}
}

func TestHistGravityIsAFeatureFlag(t *testing.T) {
	var p Player
	p.ApplyFeatures("histgravity")
	if !p.HistGravity || !evalForPlayer(p).HistGravity {
		t.Fatal("histgravity did not reach the Eval")
	}
}
