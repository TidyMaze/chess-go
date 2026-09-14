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
	if pinned.Len() != 1 || !pinned.Has(board.Sq{4, 3}) {
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

func TestAttackerOfType(t *testing.T) {
	squares := map[board.Sq]board.Piece{
		{4, 0}: {board.White, board.King},
		{2, 1}: {board.White, board.Knight},
		{3, 2}: {board.White, board.Pawn},
		{3, 3}: {board.Black, board.Pawn},
	}
	b := boardFrom(squares)

	// Square {3, 3}: attacked by White pawn at {2, 2}? No. White pawn is at {3, 2}, which does not attack {3, 3} diagonally.
	// Wait, white pawn at {3, 2} attacks {2, 3} and {4, 3}!
	// Knight at {2, 1} attacks {3, 3} (file +1, rank +2).
	sq, ok := AttackerOfType(&b, board.Sq{3, 3}, board.White, board.Knight)
	if !ok || sq != (board.Sq{2, 1}) {
		t.Errorf("expected knight at {2,1}, got %v ok=%v", sq, ok)
	}

	// Square {4, 3}: attacked by White pawn at {3, 2}.
	sq, ok = AttackerOfType(&b, board.Sq{4, 3}, board.White, board.Pawn)
	if !ok || sq != (board.Sq{3, 2}) {
		t.Errorf("expected pawn at {3,2}, got %v ok=%v", sq, ok)
	}

	// Square {5, 0}: attacked by White King at {4, 0}.
	sq, ok = AttackerOfType(&b, board.Sq{5, 0}, board.White, board.King)
	if !ok || sq != (board.Sq{4, 0}) {
		t.Errorf("expected king at {4,0}, got %v ok=%v", sq, ok)
	}
}

func BenchmarkAttackerOfType(b *testing.B) {
	bd := board.Initial()
	e4 := board.Sq{4, 3}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AttackerOfType(&bd, e4, board.White, board.Knight)
		AttackerOfType(&bd, e4, board.White, board.Pawn)
	}
}

func BenchmarkPawnMoves(b *testing.B) {
	boardState := board.Initial()
	var buf []board.Sq
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = AppendLegalTargets(buf[:0], &boardState, board.Sq{File: 4, Rank: 1}, board.White, board.Pawn)
	}
}

