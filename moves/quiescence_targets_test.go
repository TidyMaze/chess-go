package moves

import (
	"math/rand"
	"testing"

	"chess/board"
)

// quiescenceTargetsByWalk is the reference: the cell walks and the full
// pawn generator AppendQuiescenceTargets used before the bitboards, then
// the filter AppendQuiescenceMoves applied to their output, keeping
// captures, en passant and moves to the last rank.
func quiescenceTargetsByWalk(b *board.Board, sq board.Sq, color board.Color, pt board.PieceType) []board.Sq {
	var raw []board.Sq
	switch pt {
	case board.Pawn:
		raw = b.AppendPawnMoves(nil, sq, color)
	case board.Knight:
		raw = b.AppendKnightCaptures(nil, sq, color)
	case board.King:
		raw = b.AppendKingCaptures(nil, sq, color)
	case board.Bishop:
		raw = b.AppendSlideCaptures(nil, sq, color, bishopDirSlice)
	case board.Rook:
		raw = b.AppendSlideCaptures(nil, sq, color, rookDirSlice)
	case board.Queen:
		raw = b.AppendSlideCaptures(nil, sq, color, queenDirSlice)
	}
	ep, hasEP := b.EPSquare()
	enemy := b.ColorBitboard(color.Other())
	var out []board.Sq
	for _, t := range raw {
		occupied := enemy&(1<<(t.Rank*8+t.File)) != 0
		epCapture := hasEP && pt == board.Pawn && t == ep && sq.File != t.File
		promotes := pt == board.Pawn && (t.Rank == 0 || t.Rank == 7)
		if occupied || epCapture || promotes {
			out = append(out, t)
		}
	}
	return out
}

// The order matters as much as the set: move ordering breaks ties on the
// index a move was generated at, so a reordered list changes the tree.
func TestQuiescenceTargetsMatchTheFilteredWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(37))
	compared, promotions, eps := 0, 0, 0
	for trial := 0; trial < 40000; trial++ {
		b := sliderHeavyBoard(rng)
		// Pawns on the ranks next to promotion, and an en passant square
		// now and then, whether or not a pawn could have made it.
		for n := rng.Intn(6); n > 0; n-- {
			sq := board.Sq{File: int8(rng.Intn(8)), Rank: int8(1 + 5*rng.Intn(2))}
			if _, ok := b.PieceAt(sq); !ok && b.PieceCount() < 32 {
				b.Place(sq, board.Piece{Color: board.Color(rng.Intn(2)), Type: board.Pawn})
			}
		}
		if rng.Intn(4) != 0 {
			ep := board.Sq{File: int8(rng.Intn(8)), Rank: int8(2 + 3*rng.Intn(2))}
			if _, ok := b.PieceAt(ep); !ok {
				b.SetEPSquare(ep, true)
			}
		}
		ep, hasEP := b.EPSquare()
		for _, c := range []board.Color{board.White, board.Black} {
			for _, ps := range b.AppendPiecesOf(nil, c) {
				want := quiescenceTargetsByWalk(&b, ps.Sq, c, ps.Type)
				got := AppendQuiescenceTargets(nil, &b, ps.Sq, c, ps.Type)
				if len(got) != len(want) {
					t.Fatalf("trial %d, %v %v on %v: got %v, want %v", trial, c, ps.Type, ps.Sq, got, want)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("trial %d, %v %v on %v: got %v, want %v", trial, c, ps.Type, ps.Sq, got, want)
					}
					if ps.Type == board.Pawn && (want[i].Rank == 0 || want[i].Rank == 7) {
						promotions++
					}
					if hasEP && ps.Type == board.Pawn && want[i] == ep {
						eps++
					}
				}
				compared++
			}
		}
	}
	if promotions < 1000 || eps < 1000 {
		t.Fatalf("only %d promotions and %d en passant captures met", promotions, eps)
	}
	t.Logf("%d pieces compared, %d promotions, %d en passant", compared, promotions, eps)
}

func BenchmarkQuiescenceTargets(b *testing.B) {
	rng := rand.New(rand.NewSource(5))
	type job struct {
		b  board.Board
		ps []board.PieceAtSquare
	}
	jobs := make([]job, 256)
	for i := range jobs {
		jobs[i].b = sliderHeavyBoard(rng)
		jobs[i].ps = jobs[i].b.AppendPiecesOf(nil, board.White)
	}
	var buf [28]board.Sq
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := &jobs[i&255]
		for _, ps := range j.ps {
			_ = AppendQuiescenceTargets(buf[:0], &j.b, ps.Sq, board.White, ps.Type)
		}
	}
}
