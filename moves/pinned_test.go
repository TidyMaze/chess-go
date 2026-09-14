package moves

import (
	"testing"

	"chess/board"
)

func TestPinnedSetBitset(t *testing.T) {
	var s PinnedSet
	if s.Len() != 0 {
		t.Errorf("empty set should have Len 0, got %d", s.Len())
	}
	sq1 := board.Sq{File: 4, Rank: 1}
	sq2 := board.Sq{File: 2, Rank: 5}

	if s.Has(sq1) || s.Has(sq2) {
		t.Errorf("empty set should not have sq1 or sq2")
	}

	s.add(sq1)
	if s.Len() != 1 || !s.Has(sq1) || s.Has(sq2) {
		t.Errorf("set should have sq1 only, len=%d", s.Len())
	}

	s.add(sq2)
	if s.Len() != 2 || !s.Has(sq1) || !s.Has(sq2) {
		t.Errorf("set should have sq1 and sq2, len=%d", s.Len())
	}
}

func BenchmarkPinnedSetHas(b *testing.B) {
	var s PinnedSet
	sq1 := board.Sq{File: 4, Rank: 1}
	sq2 := board.Sq{File: 2, Rank: 5}
	s.add(sq1)
	s.add(sq2)
	testSq := board.Sq{File: 4, Rank: 1}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Has(testSq)
	}
}

func BenchmarkPinnedSquares(b *testing.B) {
	brd := boardFrom(map[board.Sq]board.Piece{
		{File: 4, Rank: 0}: {Color: board.White, Type: board.King},
		{File: 4, Rank: 1}: {Color: board.White, Type: board.Pawn},
		{File: 4, Rank: 7}: {Color: board.Black, Type: board.Rook},
		{File: 0, Rank: 7}: {Color: board.Black, Type: board.King},
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = PinnedSquares(&brd, board.White)
	}
}
