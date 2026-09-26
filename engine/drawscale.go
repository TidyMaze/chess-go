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
	if wrongRookPawn(b, strong) {
		return 0
	}
	count := func(c board.Color, t board.PieceType) int { return bits.OnesCount64(b.PieceBitboard(c, t)) }
	if count(strong, board.Pawn) == 0 {
		minors := count(strong, board.Knight) + count(strong, board.Bishop)
		majors := count(strong, board.Rook) + count(strong, board.Queen)
		if majors == 0 && (minors <= 1 || count(strong, board.Bishop) == 0) {
			return 0 // one minor, or knights alone, cannot force mate
		}
		// A queen against at most two minor pieces is short of the margin
		// and still mostly won: the tablebase wins 78, 71 and 59 of 80
		// quiet positions against bishop and knight, two bishops and two
		// knights. Three minors are a different ending and stay scaled.
		queenVsMinors := count(strong, board.Queen) > 0 &&
			count(weak, board.Rook)+count(weak, board.Queen) == 0 &&
			count(weak, board.Knight)+count(weak, board.Bishop) <= 2
		if !queenVsMinors && pieceMaterial(b, strong)-pieceMaterial(b, weak) < 4 {
			return score / 4
		}
	}
	if oppositeBishopsOnly(b) {
		return score / 2
	}
	return score
}

// wrongRookPawn: the strong side has pawns on one rook file only and
// bishops (if any) that can never cover the queening corner, against a
// bare king on or next to that corner. Nothing drives the king out, so it
// is a dead draw, which the network scored at +6 to +9: the engine traded
// rooks into it with seven winning moves on the board.
func wrongRookPawn(b *board.Board, strong board.Color) bool {
	weak := strong.Other()
	for t := board.Pawn; t <= board.Queen; t++ {
		if b.PieceBitboard(weak, t) != 0 {
			return false
		}
	}
	if b.PieceBitboard(strong, board.Knight)|b.PieceBitboard(strong, board.Rook)|b.PieceBitboard(strong, board.Queen) != 0 {
		return false
	}
	const fileA = 0x0101010101010101
	pawns := b.PieceBitboard(strong, board.Pawn)
	file := 0
	switch {
	case pawns != 0 && pawns&^fileA == 0:
	case pawns != 0 && pawns&^(fileA<<7) == 0:
		file = 7
	default:
		return false
	}
	rank := 7
	if strong == board.Black {
		rank = 0
	}
	for bb := b.PieceBitboard(strong, board.Bishop); bb != 0; bb &= bb - 1 {
		sq := bits.TrailingZeros64(bb)
		if (sq%8+sq/8)&1 == (file+rank)&1 {
			return false // this bishop covers the corner
		}
	}
	k := bits.TrailingZeros64(b.PieceBitboard(weak, board.King))
	df, dr := k%8-file, k/8-rank
	return df >= -1 && df <= 1 && dr >= -1 && dr <= 1
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

// kingDriveThreshold is the lead from which the king drive applies.
func kingDriveThreshold(b *board.Board, ev *Eval) float64 {
	if ev != nil && ev.Drive2 && pieceMaterial(b, board.White)+pieceMaterial(b, board.Black) <= 16 {
		return 2
	}
	return 4
}
