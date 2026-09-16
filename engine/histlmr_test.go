package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// Late move reductions treat every quiet move alike once it is late
// enough. The history tables already know which quiet moves keep causing
// cutoffs, so a move history likes should be reduced less and a move it
// dislikes reduced more. That is the standard contextual adjustment, and
// it is what shrinks the tree without losing the good move.
func TestHistoryAdjustsTheReduction(t *testing.T) {
	g := game.New()
	ctx := searchCtxPool.Get().(*searchCtx)
	defer searchCtxPool.Put(ctx)
	ctx.reset()
	ctx.ev = &Eval{ScaledLMR: true, HistLMR: true}

	m := game.Move{From: board.Sq{File: 6, Rank: 0}, To: board.Sq{File: 5, Rank: 2}} // Nf3
	const depth, index = 8, 6
	base := lmrReduction(depth, index, false, false, false)

	neutral := ctx.historyAdjustedReduction(base, board.White, g, m, depth)
	if neutral != base {
		t.Errorf("with no history the reduction should be the plain one: %d, want %d", neutral, base)
	}

	ctx.history[board.White][sqIndex(m.From)][sqIndex(m.To)] = 1 << 20
	loved := ctx.historyAdjustedReduction(base, board.White, g, m, depth)
	if loved >= base {
		t.Errorf("a move with strong history should be reduced less: %d, base %d", loved, base)
	}

	ctx.history[board.White][sqIndex(m.From)][sqIndex(m.To)] = -(1 << 20)
	hated := ctx.historyAdjustedReduction(base, board.White, g, m, depth)
	if hated <= base {
		t.Errorf("a move with bad history should be reduced more: %d, base %d", hated, base)
	}

	// Never past the bounds the plain reduction respects.
	if hated > depth-2 {
		t.Errorf("reduction %d exceeds depth-2 (%d)", hated, depth-2)
	}
	if loved < 0 {
		t.Errorf("reduction %d is negative", loved)
	}

	// Off, the adjustment must not move anything.
	ctx.ev.HistLMR = false
	if got := ctx.historyAdjustedReduction(base, board.White, g, m, depth); got != base {
		t.Errorf("with the flag off the reduction is %d, want the plain %d", got, base)
	}
}

func TestHistLMRIsAFeatureFlag(t *testing.T) {
	var p Player
	p.ApplyFeatures("histlmr")
	if !p.HistLMR || !evalForPlayer(p).HistLMR {
		t.Fatal("histlmr did not reach the Eval")
	}
}
