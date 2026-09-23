package engine

import (
	"math/bits"

	"chess/board"
)

// drawScale shrinks a score toward the draw in endgames the side ahead cannot
// usually win, whatever the network says about them. The network learns from
// this engine's own search, which cannot see these draws either.
func drawScale(b *board.Board, score float64, maximizingFor board.Color) float64 {
	strong := maximizingFor
	if score < 0 {
		strong = maximizingFor.Other()
	}
	weak := strong.Other()
	count := func(c board.Color, t board.PieceType) int { return bits.OnesCount64(b.PieceBitboard(c, t)) }
	if count(strong, board.Pawn) == 0 {
		minors := count(strong, board.Knight) + count(strong, board.Bishop)
		majors := count(strong, board.Rook) + count(strong, board.Queen)
		if majors == 0 && (minors <= 1 || count(strong, board.Bishop) == 0) {
			return 0 // one minor, or knights alone, cannot force mate
		}
		if pieceMaterial(b, strong)-pieceMaterial(b, weak) < 4 {
			return score / 4
		}
	}
	if oppositeBishopsOnly(b) {
		return score / 2
	}
	return score
}

// pieceMaterial is the non-pawn material of one side, in pawns.
func pieceMaterial(b *board.Board, c board.Color) float64 {
	n := func(t board.PieceType) float64 { return float64(bits.OnesCount64(b.PieceBitboard(c, t))) }
	return 3*n(board.Knight) + 3.3*n(board.Bishop) + 5*n(board.Rook) + 9*n(board.Queen)
}

// oppositeBishopsOnly: each side has one bishop, on different colours, and
// nothing else but pawns.
func oppositeBishopsOnly(b *board.Board) bool {
	const darkSquares = 0xAA55AA55AA55AA55
	var bishop [2]uint64
	for c := board.White; c <= board.Black; c++ {
		for _, t := range []board.PieceType{board.Knight, board.Rook, board.Queen} {
			if b.PieceBitboard(c, t) != 0 {
				return false
			}
		}
		bishop[c] = b.PieceBitboard(c, board.Bishop)
		if bits.OnesCount64(bishop[c]) != 1 {
			return false
		}
	}
	return (bishop[0]&darkSquares != 0) != (bishop[1]&darkSquares != 0)
}
