package moves

import (
	"testing"

	"chess/board"
)

func TestAppendLegalTargetsPanicsOnAnUnknownPieceType(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("an unknown piece type did not panic")
		}
	}()
	b := board.Initial()
	AppendLegalTargets(nil, &b, board.Sq{File: 0, Rank: 0}, board.White, board.PieceType(99))
}
