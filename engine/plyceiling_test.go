package engine

import (
	"testing"

	"chess/game"
)

// Extensions stop the depth decreasing, so a line of forcing moves can
// walk the search past the end of the per-ply arrays. Every write was
// guarded by ply < maxSearchPly and one read was not, which is a crash
// waiting for a feature that extends often enough to reach it: found by
// singular extensions at ply 64 of a 64-entry array, after 120 games.
//
// The fix is a ceiling at the entry rather than a guard on each access:
// there is nothing useful to search at ply 64, and returning the position's
// evaluation is what the search does at any other leaf.
func TestSearchStopsAtThePlyCeiling(t *testing.T) {
	g, err := game.ParseFEN("4k3/8/8/8/8/8/8/R3K2R w KQ - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := searchCtxPool.Get().(*searchCtx)
	defer searchCtxPool.Put(ctx)
	ctx.reset()
	ev := evalForPlayer(Strong(4))
	ev.Table = NewTranspositionTable(12)
	ctx.ev, ctx.quiescence = ev, true
	ev.acc = &ctx.acc

	// Called at the ceiling itself: the guarded arrays must not be touched.
	// Without the ceiling this reads c.fifty[64] and panics.
	got := ctx.searchNull(g, g.Turn, g.Turn, 4, maxSearchPly, negInf, posInf, false)
	if got != got { // NaN check
		t.Errorf("the ceiling returned NaN")
	}

	// One below the ceiling must still search normally.
	ctx.reset()
	ctx.ev, ctx.quiescence = ev, true
	ev.acc = &ctx.acc
	_ = ctx.searchNull(g, g.Turn, g.Turn, 2, maxSearchPly-1, negInf, posInf, false)
}
