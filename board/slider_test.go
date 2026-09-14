package board

import (
	"math/rand"
	"testing"
)

var bishopDirsForTest = [][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
var rookDirsForTest = [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

// crowdedBoard puts a random scatter of both colours on the board. Sliders
// are entirely about where they stop, so an empty board would exercise none
// of the logic that matters: the first blocker is included when it is an
// enemy and excluded when it is your own.
func crowdedBoard(rng *rand.Rand) *Board {
	// Only remove men, never add: the board keeps its occupied squares in a
	// fixed 32-entry array, so a 33rd piece panics. Thinning the initial
	// position from both sides gives plenty of blocker variety, which is all
	// a slider test needs.
	b := Initial()
	for i := 0; i < 20; i++ {
		sq := squareFromIndex(uint8(rng.Intn(64)))
		if _, occ := b.PieceAt(sq); occ {
			b.Remove(sq)
		}
	}
	return &b
}

func bitboardOf(squares []Sq) uint64 {
	var bb uint64
	for _, s := range squares {
		bb |= 1 << squareIndex(s)
	}
	return bb
}

// The ray tables must reach exactly the squares the one-square-at-a-time walk
// reaches, on every square, for both colours, over crowded boards. Own-piece
// blockers are subtracted from the table result because the table stops on a
// blocker but keeps it, which is what a capture is.
func TestSliderAttackTablesMatchTheRayWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	cases := []struct {
		name    string
		attacks func(uint8, uint64) uint64
		dirs    [][2]int
	}{
		{"bishop", BishopAttacks, bishopDirsForTest},
		{"rook", RookAttacks, rookDirsForTest},
	}
	for _, c := range cases {
		for trial := 0; trial < 120; trial++ {
			b := crowdedBoard(rng)
			for i := 0; i < 64; i++ {
				sq := squareFromIndex(uint8(i))
				for _, col := range []Color{White, Black} {
					got := c.attacks(uint8(i), b.occupiedBB()) &^ b.colorBB[col]
					want := bitboardOf(b.AppendSlideMoves(nil, sq, col, c.dirs))
					if got != want {
						t.Errorf("%s from %v as %v: %#016x, want %#016x", c.name, sq, col, got, want)
						return
					}
				}
			}
		}
	}
}

// isAttackedByRayWalk is the implementation IsAttackedBy used before the ray
// tables: step one square at a time down each of the eight slider directions
// and see whether the first man met is an enemy slider of the right kind. It
// stays here as the oracle, so a wrong table shows up as a disagreement
// rather than as a perft failure ten plies deep.
func isAttackedByRayWalk(b *Board, sq Sq, by Color) bool {
	sqIdx := squareIndex(sq)
	if PawnAttacksTo[by][sqIdx]&b.pieces[by][Pawn] != 0 {
		return true
	}
	if KnightAttacks[sqIdx]&b.pieces[by][Knight] != 0 {
		return true
	}
	if KingAttacks[sqIdx]&(1<<squareIndex(b.kings[by])) != 0 {
		return true
	}
	baseIdx := index(sq)
	for _, d := range rookDeltas {
		if b.hitsSliderDelta(baseIdx, d, by, Rook, Queen) {
			return true
		}
	}
	for _, d := range bishopDeltas {
		if b.hitsSliderDelta(baseIdx, d, by, Bishop, Queen) {
			return true
		}
	}
	return false
}

// IsAttackedBy decides every legality check and every check test in the
// engine, so a disagreement here is a wrong game, not a slow one.
func TestIsAttackedByMatchesTheRayWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 300; trial++ {
		b := crowdedBoard(rng)
		for i := 0; i < 64; i++ {
			sq := squareFromIndex(uint8(i))
			for _, by := range []Color{White, Black} {
				if got, want := b.IsAttackedBy(sq, by), isAttackedByRayWalk(b, sq, by); got != want {
					t.Errorf("IsAttackedBy(%v, %v) = %v, want %v", sq, by, got, want)
					return
				}
			}
		}
	}
}

// A ray must stop at the first blocker and not jump it. The random tests
// above would catch this, but only by accident and with an unreadable
// failure, so it is pinned here on a position built for it.
func TestARayStopsAtTheFirstBlocker(t *testing.T) {
	b := Initial()
	for i := 0; i < 64; i++ {
		b.Remove(squareFromIndex(uint8(i)))
	}
	// White rook a1, white pawn a3, black pawn a5. Up the a-file the rook
	// reaches a2 only: a3 is its own man, and a5 is behind him.
	b.Place(Sq{File: 0, Rank: 0}, Piece{Color: White, Type: Rook})
	b.Place(Sq{File: 0, Rank: 2}, Piece{Color: White, Type: Pawn})
	b.Place(Sq{File: 0, Rank: 4}, Piece{Color: Black, Type: Pawn})

	up := RookAttacks(squareIndex(Sq{File: 0, Rank: 0}), b.occupiedBB()) &^ b.colorBB[White]
	if up&(1<<squareIndex(Sq{File: 0, Rank: 1})) == 0 {
		t.Error("the rook cannot reach a2, which is empty")
	}
	if up&(1<<squareIndex(Sq{File: 0, Rank: 2})) != 0 {
		t.Error("the rook captured its own pawn on a3")
	}
	if up&(1<<squareIndex(Sq{File: 0, Rank: 4})) != 0 {
		t.Error("the rook jumped over a3 to reach a5")
	}
}

// And it must include an enemy blocker, which is the capture.
func TestARayIncludesAnEnemyBlocker(t *testing.T) {
	b := Initial()
	for i := 0; i < 64; i++ {
		b.Remove(squareFromIndex(uint8(i)))
	}
	b.Place(Sq{File: 0, Rank: 0}, Piece{Color: White, Type: Rook})
	b.Place(Sq{File: 0, Rank: 3}, Piece{Color: Black, Type: Pawn})
	up := RookAttacks(squareIndex(Sq{File: 0, Rank: 0}), b.occupiedBB())
	if up&(1<<squareIndex(Sq{File: 0, Rank: 3})) == 0 {
		t.Error("the rook cannot capture the enemy pawn on a4")
	}
	if up&(1<<squareIndex(Sq{File: 0, Rank: 4})) != 0 {
		t.Error("the rook reached a5, past the enemy pawn on a4")
	}
}

func BenchmarkIsAttackedBy(bench *testing.B) {
	b := Initial()
	b.Remove(Sq{File: 3, Rank: 1})
	b.Place(Sq{File: 3, Rank: 3}, Piece{Color: White, Type: Queen})
	sq := Sq{File: 4, Rank: 5}
	bench.ResetTimer()
	for i := 0; i < bench.N; i++ {
		_ = b.IsAttackedBy(sq, White)
	}
}
