package engine

import (
	"chess/board"
	"chess/game"
	"testing"
)

func TestShelterDoesNotIncludePassedKingPenalty(t *testing.T) {
	// In an endgame with an outside passed pawn, Shelter (middlegame castled shelter)
	// must NOT trigger passedKingPenalty. Only PassedKing should trigger it.
	g, err := game.ParseFEN("8/8/8/8/k7/8/7P/4K3 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}

	scPlain := PositionScoreEval(&g.Board, board.White, &Eval{})
	scShelter := PositionScoreEval(&g.Board, board.White, &Eval{Shelter: true})
	scPassedKing := PositionScoreEval(&g.Board, board.White, &Eval{PassedKing: true})

	if scShelter != scPlain {
		t.Errorf("Shelter should have zero effect in an endgame without castled kings: got %.3f, want %.3f", scShelter, scPlain)
	}
	if scPassedKing == scPlain {
		t.Errorf("PassedKing should affect endgame score: got %.3f", scPassedKing)
	}
}
