package board

import (
	"math/rand"
	"sort"
	"testing"
)

// sortedTargets makes two generators comparable regardless of the order
// they emit squares in. A bitboard walks squares in index order and an
// offset list walks them in the order the offsets happen to be written, so
// the sets must match even though the sequences will not.
func sortedTargets(in []Sq) []int {
	out := make([]int, 0, len(in))
	for _, s := range in {
		out = append(out, int(squareIndex(s)))
	}
	sort.Ints(out)
	return out
}

func sameTargets(t *testing.T, what string, sq Sq, got, want []Sq) {
	t.Helper()
	g, w := sortedTargets(got), sortedTargets(want)
	if len(g) != len(w) {
		t.Errorf("%s from %v: %d targets, want %d (%v vs %v)", what, sq, len(g), len(w), g, w)
		return
	}
	for i := range g {
		if g[i] != w[i] {
			t.Errorf("%s from %v: targets %v, want %v", what, sq, g, w)
			return
		}
	}
}

// randomBoard scatters men of both colours so the generators are compared
// against real occupancy, not just an empty board: the whole job of these
// functions is deciding which targets are blocked by your own pieces.
func randomBoard(rng *rand.Rand) *Board {
	b := Initial()
	for i := 0; i < 24; i++ {
		sq := squareFromIndex(uint8(rng.Intn(64)))
		if _, occ := b.PieceAt(sq); occ {
			b.Remove(sq)
		}
	}
	return &b
}

// The knight table and the offset walk must agree everywhere. The table has
// existed in bitboard.go since before this change and was not being used by
// move generation at all.
func TestKnightTableMatchesTheOffsetWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 200; trial++ {
		b := randomBoard(rng)
		for i := 0; i < 64; i++ {
			sq := squareFromIndex(uint8(i))
			for _, c := range []Color{White, Black} {
				got := b.AppendKnightMoves(nil, sq, c)
				want := b.AppendStepMoves(nil, sq, c, knightOffsetsForTest)
				sameTargets(t, "knight", sq, got, want)
			}
		}
		if t.Failed() {
			return
		}
	}
}

func TestKingTableMatchesTheOffsetWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for trial := 0; trial < 200; trial++ {
		b := randomBoard(rng)
		for i := 0; i < 64; i++ {
			sq := squareFromIndex(uint8(i))
			for _, c := range []Color{White, Black} {
				got := b.AppendKingMoves(nil, sq, c)
				want := b.AppendStepMoves(nil, sq, c, kingOffsetsForTest)
				sameTargets(t, "king", sq, got, want)
			}
		}
		if t.Failed() {
			return
		}
	}
}

// The offsets the move package passes in, duplicated here so the board
// package can compare the two generators without importing it.
var knightOffsetsForTest = [][2]int{{1, 2}, {1, -2}, {-1, 2}, {-1, -2}, {2, 1}, {2, -1}, {-2, 1}, {-2, -1}}
var kingOffsetsForTest = [][2]int{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}

func BenchmarkKnightMovesOffsets(bench *testing.B) {
	b := Initial()
	sq := Sq{File: 3, Rank: 3}
	buf := make([]Sq, 0, 8)
	bench.ResetTimer()
	for i := 0; i < bench.N; i++ {
		buf = b.AppendStepMoves(buf[:0], sq, White, knightOffsetsForTest)
	}
	_ = buf
}

func BenchmarkKnightMovesTable(bench *testing.B) {
	b := Initial()
	sq := Sq{File: 3, Rank: 3}
	buf := make([]Sq, 0, 8)
	bench.ResetTimer()
	for i := 0; i < bench.N; i++ {
		buf = b.AppendKnightMoves(buf[:0], sq, White)
	}
	_ = buf
}
