package board

import (
	"math/rand"
	"testing"
)

// A target count is a set question, so the bitboard must hold exactly the
// squares the walk appends, on every square, both colours, crowded boards.
func TestTargetBitboardMatchesTheWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	for trial := 0; trial < 120; trial++ {
		b := crowdedBoard(rng)
		for i := 0; i < 64; i++ {
			sq := squareFromIndex(uint8(i))
			for _, c := range []Color{White, Black} {
				cases := []struct {
					pt   PieceType
					want []Sq
				}{
					{Knight, b.AppendStepMoves(nil, sq, c, knightOffsetsForTest)},
					{Bishop, b.AppendSlideMoves(nil, sq, c, bishopDirsForTest)},
					{Rook, b.AppendSlideMoves(nil, sq, c, rookDirsForTest)},
					{Queen, b.AppendSlideMoves(nil, sq, c, append(append([][2]int{}, bishopDirsForTest...), rookDirsForTest...))},
				}
				for _, k := range cases {
					got, ok := b.TargetBitboard(sq, c, k.pt)
					if !ok {
						t.Fatalf("%v on %v: no bitboard", k.pt, sq)
					}
					if want := bitboardOf(k.want); got != want {
						t.Fatalf("%v on %v as %v: %#016x, want %#016x", k.pt, sq, c, got, want)
					}
				}
			}
		}
	}
}

func TestPawnsAndKingsStayOnTheWalk(t *testing.T) {
	b := Initial()
	for _, pt := range []PieceType{Pawn, King} {
		if _, ok := b.TargetBitboard(Sq{File: 4, Rank: 1}, White, pt); ok {
			t.Errorf("%v should not have a target bitboard", pt)
		}
	}
}

// Alignment ignores blockers by definition, so brute force is: any enemy
// rook or queen on the king's rank or file, any enemy bishop or queen on
// one of its diagonals.
func TestAlignedSlidersMatchesABruteForceScan(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	for trial := 0; trial < 200; trial++ {
		b := crowdedBoard(rng)
		for _, c := range []Color{White, Black} {
			king := b.KingSquare(c)
			if _, occ := b.PieceAt(king); !occ {
				continue // the king was thinned away
			}
			enemy := c.Other()
			wantR, wantB := false, false
			for i := 0; i < 64; i++ {
				sq := squareFromIndex(uint8(i))
				p, occ := b.PieceAt(sq)
				if !occ || p.Color != enemy {
					continue
				}
				sameLine := sq.File == king.File || sq.Rank == king.Rank
				sameDiag := abs(sq.File-king.File) == abs(sq.Rank-king.Rank) && sq != king
				if (p.Type == Rook || p.Type == Queen) && sameLine {
					wantR = true
				}
				if (p.Type == Bishop || p.Type == Queen) && sameDiag {
					wantB = true
				}
			}
			gotR, gotB := b.AlignedSliders(king, enemy)
			if gotR != wantR || gotB != wantB {
				t.Fatalf("king %v vs %v: rooks %v bishops %v, want %v %v", king, enemy, gotR, gotB, wantR, wantB)
			}
		}
	}
}
