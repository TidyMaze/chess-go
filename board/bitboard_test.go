package board

import (
	"math/bits"
	"testing"
)

func TestKnightAttacksCount(t *testing.T) {
	// A1 (0, 0): index 0. Attacks B3 (1, 2) and C2 (2, 1) -> 2 attacks.
	a1 := squareIndex(Sq{0, 0})
	if cnt := bits.OnesCount64(KnightAttacks[a1]); cnt != 2 {
		t.Errorf("expected 2 knight attacks from a1, got %d", cnt)
	}

	// D4 (3, 3): index 27. Attacks 8 squares.
	d4 := squareIndex(Sq{3, 3})
	if cnt := bits.OnesCount64(KnightAttacks[d4]); cnt != 8 {
		t.Errorf("expected 8 knight attacks from d4, got %d", cnt)
	}
}

func TestKingAttacksCount(t *testing.T) {
	// A1 (0, 0): index 0. Attacks A2, B1, B2 -> 3 attacks.
	a1 := squareIndex(Sq{0, 0})
	if cnt := bits.OnesCount64(KingAttacks[a1]); cnt != 3 {
		t.Errorf("expected 3 king attacks from a1, got %d", cnt)
	}

	// D4 (3, 3): attacks 8 squares.
	d4 := squareIndex(Sq{3, 3})
	if cnt := bits.OnesCount64(KingAttacks[d4]); cnt != 8 {
		t.Errorf("expected 8 king attacks from d4, got %d", cnt)
	}
}

func TestPawnAttacks(t *testing.T) {
	// White pawn on D4 (3, 3): attacks C5 (2, 4) and E5 (4, 4)
	d4 := squareIndex(Sq{3, 3})
	wAttacks := PawnAttacks[White][d4]
	c5 := squareIndex(Sq{2, 4})
	e5 := squareIndex(Sq{4, 4})
	if wAttacks != (1<<c5 | 1<<e5) {
		t.Errorf("unexpected white pawn attacks from d4: %064b", wAttacks)
	}

	// Black pawn on D4 (3, 3): attacks C3 (2, 2) and E3 (4, 2)
	bAttacks := PawnAttacks[Black][d4]
	c3 := squareIndex(Sq{2, 2})
	e3 := squareIndex(Sq{4, 2})
	if bAttacks != (1<<c3 | 1<<e3) {
		t.Errorf("unexpected black pawn attacks from d4: %064b", bAttacks)
	}
}

func TestBoardPieceBitboardsSync(t *testing.T) {
	b := Initial()

	// Initial board should have 8 pawns each
	if cnt := bits.OnesCount64(b.PieceBitboard(White, Pawn)); cnt != 8 {
		t.Errorf("expected 8 white pawns, got %d", cnt)
	}
	if cnt := bits.OnesCount64(b.PieceBitboard(Black, Pawn)); cnt != 8 {
		t.Errorf("expected 8 black pawns, got %d", cnt)
	}

	// Test Move and UnmakeMove preserves bitboards
	u := b.MakeMove(Sq{4, 1}, Sq{4, 3}) // e2 to e4
	if (b.PieceBitboard(White, Pawn) & (1 << squareIndex(Sq{4, 3}))) == 0 {
		t.Errorf("e4 should have white pawn bit set")
	}
	if (b.PieceBitboard(White, Pawn) & (1 << squareIndex(Sq{4, 1}))) != 0 {
		t.Errorf("e2 should not have white pawn bit set")
	}

	b.UnmakeMove(u)
	if (b.PieceBitboard(White, Pawn) & (1 << squareIndex(Sq{4, 1}))) == 0 {
		t.Errorf("e2 should have white pawn bit set after unmake")
	}
	if (b.PieceBitboard(White, Pawn) & (1 << squareIndex(Sq{4, 3}))) != 0 {
		t.Errorf("e4 should not have white pawn bit set after unmake")
	}
}
