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

func TestMakeUnmakeRestoresPosition(t *testing.T) {
	b := Initial()
	before := b
	u := b.MakeMove(Sq{4, 1}, Sq{4, 3})
	if b == before {
		t.Fatalf("MakeMove did not change the board")
	}
	b.UnmakeMove(u)
	if b != before {
		t.Errorf("UnmakeMove did not restore the board exactly")
	}
}

func TestMakeUnmakeRestoresCapture(t *testing.T) {
	b := NewEmpty()
	b.Place(Sq{0, 0}, Piece{White, King})
	b.Place(Sq{7, 7}, Piece{Black, King})
	b.Place(Sq{0, 4}, Piece{White, Rook})
	b.Place(Sq{0, 6}, Piece{Black, Queen})
	before := b
	u := b.MakeMove(Sq{0, 4}, Sq{0, 6})
	if p, ok := b.PieceAt(Sq{0, 6}); !ok || p.Type != Rook {
		t.Fatalf("capture did not apply")
	}
	b.UnmakeMove(u)
	if b != before {
		t.Errorf("UnmakeMove did not restore a capture exactly")
	}
}

func TestMakeUnmakeRestoresKingSquare(t *testing.T) {
	b := Initial()
	before := b
	u := b.MakeMove(Sq{4, 0}, Sq{4, 1})
	b.UnmakeMove(u)
	if b.KingSquare(White) != before.KingSquare(White) || b != before {
		t.Errorf("king square not restored")
	}
}

func TestBoardHitsSlider(t *testing.T) {
	b := NewEmpty()
	b.Place(Sq{4, 0}, Piece{White, King})
	b.Place(Sq{4, 7}, Piece{Black, Rook})
	if !b.HitsSlider(Sq{4, 0}, 0, 1, Black, Rook, Queen) {
		t.Errorf("expected HitsSlider to find Black rook on e8 from e1")
	}
	if b.HitsSlider(Sq{4, 0}, 0, 1, White, Rook, Queen) {
		t.Errorf("expected HitsSlider to not match White enemy")
	}
	if b.HitsSlider(Sq{4, 0}, 1, 0, Black, Rook, Queen) {
		t.Errorf("expected HitsSlider to return false when no piece on ray")
	}
	b.Place(Sq{4, 3}, Piece{White, Pawn})
	if b.HitsSlider(Sq{4, 0}, 0, 1, Black, Rook, Queen) {
		t.Errorf("expected HitsSlider to be blocked by friendly pawn")
	}
}

func TestBoardAppendSlideAndStepMoves(t *testing.T) {
	b := NewEmpty()
	b.Place(Sq{3, 3}, Piece{White, Rook})
	b.Place(Sq{3, 5}, Piece{Black, Pawn})
	dirs := [][2]int{{0, 1}, {0, -1}, {1, 0}, {-1, 0}}
	var buf [64]Sq
	slides := b.AppendSlideMoves(buf[:0], Sq{3, 3}, White, dirs)
	if len(slides) != 12 {
		t.Errorf("expected 12 rook moves, got %d: %v", len(slides), slides)
	}

	knightOffsets := [][2]int{{1, 2}, {1, -2}, {-1, 2}, {-1, -2}, {2, 1}, {2, -1}, {-2, 1}, {-2, -1}}
	steps := b.AppendStepMoves(buf[:0], Sq{3, 3}, White, knightOffsets)
	if len(steps) != 8 {
		t.Errorf("expected 8 knight steps from center, got %d: %v", len(steps), steps)
	}
}

func TestBoardFastIsInCheck(t *testing.T) {
	b := Initial()
	if b.IsInCheck(White) || b.IsInCheck(Black) {
		t.Errorf("initial board should not be in check")
	}

	// Put White king in check from Black queen on e8 -> e1
	b.Remove(Sq{4, 1}) // remove e2 pawn
	b.Remove(Sq{4, 6}) // remove e7 pawn
	b.Place(Sq{4, 7}, Piece{Black, Queen})
	if !b.IsInCheck(White) {
		t.Errorf("White king should be in check from Black queen on e8")
	}
	if b.IsInCheck(Black) {
		t.Errorf("Black king should not be in check")
	}

	// Put Black king in check from White knight
	b.Place(Sq{2, 6}, Piece{White, Knight}) // c7 knight attacks e8 king
	if !b.IsInCheck(Black) {
		t.Errorf("Black king on e8 should be in check from White knight on c7")
	}
}

func TestBoardFastIsAttackedBy(t *testing.T) {
	b := NewEmpty()
	b.Place(Sq{4, 4}, Piece{White, Pawn}) // e5
	b.Place(Sq{3, 5}, Piece{Black, Pawn}) // d6 attacks e5
	if !b.IsAttackedBy(Sq{4, 4}, Black) {
		t.Errorf("e5 should be attacked by Black pawn on d6")
	}
	if b.IsAttackedBy(Sq{4, 4}, White) {
		t.Errorf("e5 should not be attacked by White")
	}
}

func TestBoardFindPinnedPiece(t *testing.T) {
	b := NewEmpty()
	// White King on e1, White Pawn on e2, Black Rook on e8 -> e2 pawn is pinned
	b.Place(Sq{4, 0}, Piece{White, King})
	b.Place(Sq{4, 1}, Piece{White, Pawn})
	b.Place(Sq{4, 7}, Piece{Black, Rook})

	pinned, ok := b.FindPinnedPiece(Sq{4, 0}, 0, 1, White, Rook, Queen)
	if !ok || pinned != (Sq{4, 1}) {
		t.Fatalf("expected e2 to be pinned, got pinned=%v ok=%v", pinned, ok)
	}

	// Not pinned by Bishop
	_, okBishop := b.FindPinnedPiece(Sq{4, 0}, 0, 1, White, Bishop, Queen)
	if okBishop {
		t.Fatalf("expected rook not to pin when checking for bishop")
	}
}
