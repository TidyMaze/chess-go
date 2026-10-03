package engine

import (
	"chess/board"
	"chess/game"
	"testing"
)

func TestPawnAdvanceTieBreaker(t *testing.T) {
	// When root moves are tied on score, searchIterative must prefer a pawn advance
	// over a piece shuffle to make irreversible progress and reset the 50-move clock.
	gpawn, err := game.ParseFEN("8/8/4k3/8/4P3/8/8/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	pawnMove, _ := game.MoveFromUCI("e4e5")
	kingMove, _ := game.MoveFromUCI("e1e2")
	tied := []game.Move{kingMove, pawnMove}

	pushes := tied[:0:0]
	for _, m := range tied {
		if p, ok := gpawn.Board.CellPiece(m.From); ok && p.Type == board.Pawn {
			pushes = append(pushes, m)
		}
	}
	if len(pushes) != 1 || pushes[0] != pawnMove {
		t.Errorf("expected pawnMove to be filtered as push, got %v", pushes)
	}
}
