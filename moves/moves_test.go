package moves

import (
	"chess/board"
	"testing"
)

func containsSq(list []board.Sq, s board.Sq) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestPawnDoublePushFromStart(t *testing.T) {
	b := board.Initial()
	targets := LegalTargets(&b, board.Sq{4, 1}, board.White, board.Pawn)
	if len(targets) != 2 || !containsSq(targets, board.Sq{4, 2}) || !containsSq(targets, board.Sq{4, 3}) {
		t.Errorf("expected {4,2} and {4,3}, got %v", targets)
	}
}

func TestPawnBlockedByPieceAhead(t *testing.T) {
	squares := map[board.Sq]board.Piece{
		{4, 1}: {board.White, board.Pawn},
		{4, 2}: {board.Black, board.Pawn},
	}
	b := boardFrom(squares)
	targets := LegalTargets(&b, board.Sq{4, 1}, board.White, board.Pawn)
	if len(targets) != 0 {
		t.Errorf("expected no moves, got %v", targets)
	}
}

func TestKnightMovesFromCenter(t *testing.T) {
	squares := map[board.Sq]board.Piece{{3, 3}: {board.White, board.Knight}}
	b := boardFrom(squares)
	targets := LegalTargets(&b, board.Sq{3, 3}, board.White, board.Knight)
	if len(targets) != 8 {
		t.Errorf("expected 8 knight moves from center, got %d: %v", len(targets), targets)
	}
}

func TestRookBlockedByOwnPiece(t *testing.T) {
	squares := map[board.Sq]board.Piece{
		{0, 0}: {board.White, board.Rook},
		{0, 3}: {board.White, board.Pawn},
	}
	b := boardFrom(squares)
	targets := LegalTargets(&b, board.Sq{0, 0}, board.White, board.Rook)
	if containsSq(targets, board.Sq{0, 3}) || containsSq(targets, board.Sq{0, 4}) {
		t.Errorf("rook should not pass through or land on own piece: %v", targets)
	}
	if !containsSq(targets, board.Sq{0, 2}) || !containsSq(targets, board.Sq{7, 0}) {
		t.Errorf("rook should reach {0,2} and {7,0}: %v", targets)
	}
}

func TestIsInCheckFromRook(t *testing.T) {
	squares := map[board.Sq]board.Piece{
		{4, 0}: {board.White, board.King},
		{4, 7}: {board.Black, board.Rook},
		{0, 0}: {board.Black, board.King},
	}
	b := boardFrom(squares)
	if !IsInCheck(&b, board.White) {
		t.Errorf("expected white to be in check")
	}
}

func TestIsInCheckBlockedByOwnPiece(t *testing.T) {
	squares := map[board.Sq]board.Piece{
		{4, 0}: {board.White, board.King},
		{4, 3}: {board.White, board.Pawn},
		{4, 7}: {board.Black, board.Rook},
		{0, 0}: {board.Black, board.King},
	}
	b := boardFrom(squares)
	if IsInCheck(&b, board.White) {
		t.Errorf("expected white not to be in check (blocked)")
	}
}

func TestPinnedSquaresFindsPinnedPawn(t *testing.T) {
	squares := map[board.Sq]board.Piece{
		{4, 0}: {board.White, board.King},
		{4, 3}: {board.White, board.Pawn},
		{4, 7}: {board.Black, board.Rook},
		{0, 0}: {board.Black, board.King},
	}
	b := boardFrom(squares)
	pinned := PinnedSquares(&b, board.White)
	if len(pinned) != 1 || pinned[board.Sq{4, 3}] != true {
		t.Errorf("expected {4,3} pinned, got %v", pinned)
	}
}

func boardFrom(pieces map[board.Sq]board.Piece) board.Board {
	b := board.NewEmpty()
	for sq, p := range pieces {
		b.Place(sq, p)
	}
	return b
}
