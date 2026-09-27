package board

import (
	"math/rand"
	"testing"
)

// appendPiecesOfDecode is AppendPiecesOf as it was before the colour
// bitboard test: decode every occupied cell and keep the ones of colour c.
// The oracle for order as well as content, since the move list is built
// piece by piece in this order.
func appendPiecesOfDecode(b *Board, dst []PieceAtSquare, c Color) []PieceAtSquare {
	result := dst
	for i := 0; i < b.occupiedCount; i++ {
		s := squareFromIndex(b.occupied[i])
		cl := b.cells[index(s)]
		if cl >= codePieceMin {
			if p := decodePiece(cl); p.Color == c {
				result = append(result, PieceAtSquare{s, p.Type})
			}
		}
	}
	return result
}

// scrambledBoard is a crowded board after a few random piece moves, so the
// occupied list is in a play-like order rather than setup order.
func scrambledBoard(rng *rand.Rand) *Board {
	b := crowdedBoard(rng)
	for k := 0; k < 12; k++ {
		from := squareFromIndex(b.occupied[rng.Intn(b.occupiedCount)])
		to := squareFromIndex(uint8(rng.Intn(64)))
		if p, ok := b.PieceAt(to); from == to || ok && p.Type == King {
			continue
		}
		b.Move(from, to)
	}
	return b
}

func TestAppendPiecesOfMatchesTheCellDecode(t *testing.T) {
	rng := rand.New(rand.NewSource(41))
	for trial := 0; trial < 2000; trial++ {
		b := scrambledBoard(rng)
		for _, c := range []Color{White, Black} {
			prefix := []PieceAtSquare{{Sq{File: 7, Rank: 7}, Queen}}
			got := b.AppendPiecesOf(append([]PieceAtSquare(nil), prefix...), c)
			want := appendPiecesOfDecode(b, append([]PieceAtSquare(nil), prefix...), c)
			if len(got) != len(want) {
				t.Fatalf("%v pieces: %v, want %v", c, got, want)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("%v pieces: %v, want %v", c, got, want)
				}
			}
		}
	}
}

func TestSqCellIsTheCellIndex(t *testing.T) {
	for i := 0; i < 64; i++ {
		if got, want := int(sqCell[i]), index(squareFromIndex(uint8(i))); got != want {
			t.Errorf("sqCell[%d] = %d, want %d", i, got, want)
		}
	}
}
