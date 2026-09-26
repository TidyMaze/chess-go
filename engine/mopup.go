package engine

import (
	"math/bits"

	"chess/board"
)

// Mop-up weights, in pawns: per king step the lone king stands closer to a
// mating corner, and per step between the two kings. The corner weight has
// to beat the net's move-to-move noise of 0.3 to 0.8 pawns. Playing itself
// at depths 8 and 10, the bot mated 4 of 4 bishop-and-knight games at 2.0
// a step (about Stockfish's weighting for this ending), 2 at 0.2 and 1 at
// 1.0.
const (
	mopCorner = 2.0
	mopKings  = 0.2
)

// minorsMopUp is the king drive for a lone king against minor pieces that
// can mate it: bishop and knight, two bishops, or more, with no pawn. The
// mate there needs a corner, and bishop and knight only mate in a corner of
// the bishop's colour. kingDrivingBonus measures the distance from the
// centre with a Chebyshev distance, flat along a whole edge, so once the
// lone king reached an edge nothing pointed at the corner, and the net
// even scored the mating corner below the middle of the edge. This term
// counts the lone king's king steps to the nearest mating corner and the
// steps between the kings. ok is false outside these endings.
func minorsMopUp(b *board.Board, strong board.Color) (bonus float64, ok bool) {
	weak := strong.Other()
	for t := board.Pawn; t <= board.Queen; t++ {
		if b.PieceBitboard(weak, t) != 0 {
			return 0, false
		}
	}
	if b.PieceBitboard(strong, board.Pawn)|b.PieceBitboard(strong, board.Rook)|b.PieceBitboard(strong, board.Queen) != 0 {
		return 0, false
	}
	bishops := b.PieceBitboard(strong, board.Bishop)
	if bishops == 0 || bits.OnesCount64(bishops|b.PieceBitboard(strong, board.Knight)) < 2 {
		return 0, false
	}
	lone, own := b.KingSquare(weak), b.KingSquare(strong)
	f, r := lone.File, lone.Rank
	const darkSquares = 0xAA55AA55AA55AA55
	var corner int // king steps to the nearest mating corner
	switch {
	case bishops&^darkSquares == 0: // dark bishops mate on a1 and h8
		corner = 7 - iabs(7-f-r)
	case bishops&darkSquares == 0: // light bishops on a8 and h1
		corner = 7 - iabs(f-r)
	default: // both colours: any corner
		corner = min(f, 7-f) + min(r, 7-r)
	}
	kings := iabs(own.File-f) + iabs(own.Rank-r)
	return mopCorner*float64(7-corner) + mopKings*float64(14-kings), true
}

func iabs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
