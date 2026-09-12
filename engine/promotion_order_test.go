package engine

import (
	"chess/board"
	"chess/game"
	"testing"
)

func TestQuietPromotionOrderedBeforeQuiets(t *testing.T) {
	g, err := game.ParseFEN("k7/4P3/8/8/8/8/8/4K1N1 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN: %v", err)
	}

	c := &searchCtx{ev: &Eval{MainSEE: true}}
	nf3 := game.Move{From: board.Sq{File: 6, Rank: 0}, To: board.Sq{File: 5, Rank: 2}}
	c.killers[0][0] = nf3

	legal := g.AllLegalMoves(board.White)
	c.orderMoves(g, legal, game.Move{}, 0, board.White)

	promoMove := game.Move{From: board.Sq{File: 4, Rank: 6}, To: board.Sq{File: 4, Rank: 7}}
	if legal[0] != promoMove {
		t.Fatalf("expected quiet promotion %v to be first, got %v (promo score should beat killer %v)", promoMove, legal[0], nf3)
	}
}
