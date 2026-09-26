package engine

import "chess/board"

// Mop-up weights, in pawns: per step the lone king stands closer to a
// mating corner, and per step between the two kings. The corner weight has
// to beat the net's move-to-move noise of 0.3 to 0.8 pawns. Playing itself
// at depth 8 from 12 bishop-and-knight starts, the bot mated 12 at 2.0 a
// step (about Stockfish's weighting for this ending); scored by the
// mop-up alone at 0.1 to 0.5 a step, 4 to 10.
const (
	mopCorner = 2.0
	mopKings  = 0.2
)

// mopUp is the king drive against a bare king when strong has bishop and
// knight or bishops of both colours, whatever else it has. kingDrivingBonus
// measures the lone king's distance from the centre with a Chebyshev
// distance, flat along a whole edge, so once the lone king reached an edge
// nothing pointed at the corner bishop and knight need, and the net even
// scored the mating corner below the middle of the edge. This term counts
// the lone king's steps to the nearest mating corner and the steps between
// the kings.
//
// It is worth up to 16.8 pawns, so the endings the strong side can reach
// bishop and knight from by giving material away carry it too, with a
// corner at least as close. Given to bishop and knight alone, it made them
// score 23.5 against a bare king where the same position with an extra
// pawn scored 9.7, and the bot let the lone king take its pawn. Only
// bishop and knight with nothing else count the corner of the bishop's
// colour; a queen, a rook, a pawn that can queen or a second bishop mate
// in any corner. Queen and rook without these minor pieces keep the old
// drive: given the mop-up too, rook against king mated 1 of 5 times at
// depth 2, and with rook, bishop and knight against two pawns the bot gave
// its bishop for a pawn to reach a bare king. ok is false outside these
// endings.
func mopUp(b *board.Board, strong board.Color) (bonus float64, ok bool) {
	weak := strong.Other()
	for t := board.Pawn; t <= board.Queen; t++ {
		if b.PieceBitboard(weak, t) != 0 {
			return 0, false
		}
	}
	const darkSquares = 0xAA55AA55AA55AA55
	bishops := b.PieceBitboard(strong, board.Bishop)
	bothColours := bishops&darkSquares != 0 && bishops&^darkSquares != 0
	if bishops == 0 || b.PieceBitboard(strong, board.Knight) == 0 && !bothColours {
		return 0, false
	}
	majors := b.PieceBitboard(strong, board.Rook) | b.PieceBitboard(strong, board.Queen)
	lone, own := b.KingSquare(weak), b.KingSquare(strong)
	f, r := lone.File, lone.Rank
	corner := min(f, 7-f) + min(r, 7-r) // steps to the nearest corner
	if majors|b.PieceBitboard(strong, board.Pawn) == 0 && !bothColours {
		if bishops&^darkSquares == 0 {
			corner = 7 - iabs(7-f-r) // dark bishops mate on a1 and h8
		} else {
			corner = 7 - iabs(f-r) // light bishops on a8 and h1
		}
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
