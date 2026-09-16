package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// Continuation history remembers which reply refuted which move: after
// the opponent's move to square X, the quiet move of piece P to square Y
// caused a cutoff. Plain history only knows P to Y was good somewhere;
// this knows it was good against that move, which is the difference
// between a refutation and a coincidence.
func TestContinuationHistoryRanksTheRefutationOfThePreviousMove(t *testing.T) {
	g := game.New()
	ctx := searchCtxPool.Get().(*searchCtx)
	defer searchCtxPool.Put(ctx)
	ctx.reset()
	ctx.ev = &Eval{ContHist: true}

	prev := game.Move{From: board.Sq{File: 4, Rank: 6}, To: board.Sq{File: 4, Rank: 4}}  // ...e5
	reply := game.Move{From: board.Sq{File: 6, Rank: 0}, To: board.Sq{File: 5, Rank: 2}} // Nf3
	other := game.Move{From: board.Sq{File: 1, Rank: 0}, To: board.Sq{File: 2, Rank: 2}} // Nc3

	// Nf3 refuted ...e5 once at depth 5, at some other node.
	ctx.prevMove = prev
	ctx.recordHistory(board.White, g, reply, 5)

	// Now a node where ...e5 was just played: Nf3 must outrank Nc3.
	ctx.prevMove = prev
	nf3, _ := ctx.scoreMove(g, reply, game.Move{}, 3, board.White)
	nc3, _ := ctx.scoreMove(g, other, game.Move{}, 3, board.White)
	if nf3 <= nc3 {
		t.Errorf("after ...e5, Nf3 scores %d and Nc3 %d; the recorded refutation must rank higher", nf3, nc3)
	}

	// After a different previous move the continuation bonus does not apply,
	// and only plain history separates them.
	ctx.prevMove = game.Move{From: board.Sq{File: 3, Rank: 6}, To: board.Sq{File: 3, Rank: 4}} // ...d5
	nf3d5, _ := ctx.scoreMove(g, reply, game.Move{}, 3, board.White)
	if nf3d5 >= nf3 {
		t.Errorf("Nf3 after ...d5 scores %d, not below its %d after ...e5: the bonus is not conditioned on the previous move", nf3d5, nf3)
	}
}

func TestContHistIsAFeatureFlag(t *testing.T) {
	var p Player
	p.ApplyFeatures("conthist")
	if !p.ContHist || !evalForPlayer(p).ContHist {
		t.Fatal("conthist did not reach the Eval")
	}
}
