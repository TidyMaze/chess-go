package board

import (
	"math/rand"
	"testing"
)

func TestSliderTableMatchesTheSlideWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	dirsB := [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	dirsR := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	dirsQ := [8][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}}

	for trial := 0; trial < 100; trial++ {
		b := randomBoard(rng)
		for i := 0; i < 64; i++ {
			sq := squareFromIndex(uint8(i))
			for _, c := range []Color{White, Black} {
				// Bishop
				gotB := b.AppendBishopMoves(nil, sq, c)
				wantB := b.AppendSlideMoves(nil, sq, c, dirsB[:])
				sameTargets(t, "bishop", sq, gotB, wantB)

				// Rook
				gotR := b.AppendRookMoves(nil, sq, c)
				wantR := b.AppendSlideMoves(nil, sq, c, dirsR[:])
				sameTargets(t, "rook", sq, gotR, wantR)

				// Queen
				gotQ := b.AppendQueenMoves(nil, sq, c)
				wantQ := b.AppendSlideMoves(nil, sq, c, dirsQ[:])
				sameTargets(t, "queen", sq, gotQ, wantQ)
			}
		}
		if t.Failed() {
			return
		}
	}
}

func BenchmarkBishopWalk(bench *testing.B) {
	b := Initial()
	sq := Sq{File: 3, Rank: 3}
	dirs := [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	buf := make([]Sq, 0, 16)
	bench.ResetTimer()
	for i := 0; i < bench.N; i++ {
		buf = b.AppendSlideMoves(buf[:0], sq, White, dirs[:])
	}
	_ = buf
}

func BenchmarkBishopTable(bench *testing.B) {
	b := Initial()
	sq := Sq{File: 3, Rank: 3}
	buf := make([]Sq, 0, 16)
	bench.ResetTimer()
	for i := 0; i < bench.N; i++ {
		buf = b.AppendBishopMoves(buf[:0], sq, White)
	}
	_ = buf
}

func BenchmarkRookWalk(bench *testing.B) {
	b := Initial()
	sq := Sq{File: 3, Rank: 3}
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	buf := make([]Sq, 0, 16)
	bench.ResetTimer()
	for i := 0; i < bench.N; i++ {
		buf = b.AppendSlideMoves(buf[:0], sq, White, dirs[:])
	}
	_ = buf
}

func BenchmarkRookTable(bench *testing.B) {
	b := Initial()
	sq := Sq{File: 3, Rank: 3}
	buf := make([]Sq, 0, 16)
	bench.ResetTimer()
	for i := 0; i < bench.N; i++ {
		buf = b.AppendRookMoves(buf[:0], sq, White)
	}
	_ = buf
}

func BenchmarkQueenWalk(bench *testing.B) {
	b := Initial()
	sq := Sq{File: 3, Rank: 3}
	dirs := [8][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	buf := make([]Sq, 0, 32)
	bench.ResetTimer()
	for i := 0; i < bench.N; i++ {
		buf = b.AppendSlideMoves(buf[:0], sq, White, dirs[:])
	}
	_ = buf
}

func BenchmarkQueenTable(bench *testing.B) {
	b := Initial()
	sq := Sq{File: 3, Rank: 3}
	buf := make([]Sq, 0, 32)
	bench.ResetTimer()
	for i := 0; i < bench.N; i++ {
		buf = b.AppendQueenMoves(buf[:0], sq, White)
	}
	_ = buf
}
