package board

import (
	"math/rand"
	"testing"
)

// appendSlideCapturesWalk is AppendSlideCaptures as it was before the
// attack-set version: step through the padded cells direction by direction
// and keep the first man met when it is an enemy. It is the oracle for the
// order as well as the set, since alpha-beta reads the list in order.
func appendSlideCapturesWalk(b *Board, dst []Sq, sq Sq, color Color, dirs [][2]int) []Sq {
	moves := dst
	baseIdx := index(sq)
	for _, d := range dirs {
		delta := d[1]*width + d[0]
		currIdx := baseIdx
		for {
			currIdx += delta
			code := b.cells[currIdx]
			if code == codeOffBoard {
				break
			}
			if code == codeEmpty {
				continue
			}
			if cellColor[code] != color {
				moves = append(moves, cellToSq[currIdx])
			}
			break
		}
	}
	return moves
}

var unitDirsForTest = [8][2]int{{0, 1}, {0, -1}, {1, 0}, {-1, 0}, {1, 1}, {-1, 1}, {1, -1}, {-1, -1}}

// dirOf must name the ray that walking d from any square covers.
func TestDirOfNamesTheWalkedRay(t *testing.T) {
	for _, d := range unitDirsForTest {
		for i := 0; i < 64; i++ {
			f, r := i%8, i/8
			var want uint64
			for {
				f, r = f+d[0], r+d[1]
				if f < 0 || f > 7 || r < 0 || r > 7 {
					break
				}
				want |= 1 << (r*8 + f)
			}
			if got := rayAttacks[dirOf(d)][i]; got != want {
				t.Fatalf("ray of %v from %d = %#016x, want %#016x", d, i, got, want)
			}
		}
	}
}

func sameSequence(a, b []Sq) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Same squares in the same order as the walk, for every square, both
// colours, crowded boards, and every direction list a caller passes: the
// three piece lists in the order the move generator uses, a reordered
// queen, and each direction alone.
func TestAppendSlideCapturesMatchesTheWalk(t *testing.T) {
	queen := append(append([][2]int{}, bishopDirsForTest...), rookDirsForTest...)
	reversed := make([][2]int, len(queen))
	for i, d := range queen {
		reversed[len(queen)-1-i] = d
	}
	dirSets := [][][2]int{bishopDirsForTest, rookDirsForTest, queen, reversed}
	for _, d := range unitDirsForTest {
		dirSets = append(dirSets, [][2]int{d})
	}
	rng := rand.New(rand.NewSource(31))
	for trial := 0; trial < 300; trial++ {
		b := crowdedBoard(rng)
		for i := 0; i < 64; i++ {
			sq := squareFromIndex(uint8(i))
			for _, c := range []Color{White, Black} {
				for _, dirs := range dirSets {
					prefix := []Sq{{File: 7, Rank: 7}}
					got := b.AppendSlideCaptures(append([]Sq(nil), prefix...), sq, c, dirs)
					want := appendSlideCapturesWalk(b, append([]Sq(nil), prefix...), sq, c, dirs)
					if !sameSequence(got, want) {
						t.Fatalf("captures from %v as %v along %v: %v, want %v", sq, c, dirs, got, want)
					}
				}
			}
		}
	}
}
