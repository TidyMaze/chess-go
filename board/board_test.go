package board

import "testing"

func TestInitialSetupBackRank(t *testing.T) {
	b := Initial()
	order := []PieceType{Rook, Knight, Bishop, Queen, King, Bishop, Knight, Rook}
	for file, pt := range order {
		white, ok := b.PieceAt(Sq{file, 0})
		if !ok || white.Color != White || white.Type != pt {
			t.Errorf("file %d rank 0: got %+v ok=%v, want %v", file, white, ok, pt)
		}
		black, ok := b.PieceAt(Sq{file, 7})
		if !ok || black.Color != Black || black.Type != pt {
			t.Errorf("file %d rank 7: got %+v ok=%v, want %v", file, black, ok, pt)
		}
	}
}

func TestInitialSetupPawns(t *testing.T) {
	b := Initial()
	for file := 0; file < 8; file++ {
		if p, ok := b.PieceAt(Sq{file, 1}); !ok || p.Color != White || p.Type != Pawn {
			t.Errorf("file %d rank1: got %+v ok=%v", file, p, ok)
		}
		if p, ok := b.PieceAt(Sq{file, 6}); !ok || p.Color != Black || p.Type != Pawn {
			t.Errorf("file %d rank6: got %+v ok=%v", file, p, ok)
		}
	}
}

func TestInitialSetupEmptyMiddle(t *testing.T) {
	b := Initial()
	for file := 0; file < 8; file++ {
		for rank := 2; rank < 6; rank++ {
			if _, ok := b.PieceAt(Sq{file, rank}); ok {
				t.Errorf("file %d rank %d should be empty", file, rank)
			}
		}
	}
}

func TestKingSquareInitial(t *testing.T) {
	b := Initial()
	if b.KingSquare(White) != (Sq{4, 0}) {
		t.Errorf("white king square wrong: %+v", b.KingSquare(White))
	}
	if b.KingSquare(Black) != (Sq{4, 7}) {
		t.Errorf("black king square wrong: %+v", b.KingSquare(Black))
	}
}

func TestMoveTracksKing(t *testing.T) {
	b := Initial()
	b.Move(Sq{4, 0}, Sq{4, 1})
	if b.KingSquare(White) != (Sq{4, 1}) {
		t.Errorf("king square not updated after move: %+v", b.KingSquare(White))
	}
}

func TestCloneIsIndependent(t *testing.T) {
	b := Initial()
	clone := b.Clone()
	clone.Move(Sq{4, 0}, Sq{4, 1})
	if b.KingSquare(White) != (Sq{4, 0}) {
		t.Errorf("original board mutated by clone's move")
	}
	if clone.KingSquare(White) != (Sq{4, 1}) {
		t.Errorf("clone's move didn't apply")
	}
}

func TestPieceAtOffBoardReturnsFalse(t *testing.T) {
	b := Initial()
	if _, ok := b.PieceAt(Sq{-1, 0}); ok {
		t.Errorf("off-board square should return ok=false")
	}
	if _, ok := b.PieceAt(Sq{8, 0}); ok {
		t.Errorf("off-board square should return ok=false")
	}
}

func TestPiecesOfCountsSixteenAtStart(t *testing.T) {
	b := Initial()
	if len(b.PiecesOf(White)) != 16 {
		t.Errorf("expected 16 white pieces, got %d", len(b.PiecesOf(White)))
	}
	if len(b.PiecesOf(Black)) != 16 {
		t.Errorf("expected 16 black pieces, got %d", len(b.PiecesOf(Black)))
	}
}
