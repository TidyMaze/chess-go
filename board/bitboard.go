package board

import "math/bits"

// Bitboard constants and precomputed attack/structure tables.
// Square mapping: Little-Endian Rank-File (sq = rank*8 + file, 0=a1, 63=h8).

var (
	KnightAttacks     [64]uint64
	KingAttacks       [64]uint64
	PawnAttacks       [2][64]uint64 // Attacks FROM square sq for color
	PawnAttacksTo     [2][64]uint64 // Attacks TO square sq from pawns of color
	FileMask          [8]uint64
	AdjacentFilesMask [8]uint64
	PassedPawnMask    [2][64]uint64
)

func init() {
	// Initialize FileMask
	for f := 0; f < 8; f++ {
		var mask uint64
		for r := 0; r < 8; r++ {
			mask |= 1 << (r*8 + f)
		}
		FileMask[f] = mask
	}

	// Initialize AdjacentFilesMask
	for f := 0; f < 8; f++ {
		var mask uint64
		if f > 0 {
			mask |= FileMask[f-1]
		}
		if f < 7 {
			mask |= FileMask[f+1]
		}
		AdjacentFilesMask[f] = mask
	}

	// Knight attacks
	knightDeltas := [][2]int{
		{-2, -1}, {-2, 1}, {-1, -2}, {-1, 2},
		{1, -2}, {1, 2}, {2, -1}, {2, 1},
	}
	for sq := 0; sq < 64; sq++ {
		f, r := sq%8, sq/8
		var mask uint64
		for _, d := range knightDeltas {
			nf, nr := f+d[0], r+d[1]
			if nf >= 0 && nf < 8 && nr >= 0 && nr < 8 {
				mask |= 1 << (nr*8 + nf)
			}
		}
		KnightAttacks[sq] = mask
	}

	// King attacks
	kingDeltas := [][2]int{
		{-1, -1}, {-1, 0}, {-1, 1},
		{0, -1}, {0, 1},
		{1, -1}, {1, 0}, {1, 1},
	}
	for sq := 0; sq < 64; sq++ {
		f, r := sq%8, sq/8
		var mask uint64
		for _, d := range kingDeltas {
			nf, nr := f+d[0], r+d[1]
			if nf >= 0 && nf < 8 && nr >= 0 && nr < 8 {
				mask |= 1 << (nr*8 + nf)
			}
		}
		KingAttacks[sq] = mask
	}

	// Pawn attacks (From and To)
	for sq := 0; sq < 64; sq++ {
		f, r := sq%8, sq/8
		// White pawn attacks up (rank + 1)
		var wFromMask uint64
		if r+1 < 8 {
			if f > 0 {
				wFromMask |= 1 << ((r+1)*8 + (f - 1))
			}
			if f < 7 {
				wFromMask |= 1 << ((r+1)*8 + (f + 1))
			}
		}
		PawnAttacks[White][sq] = wFromMask

		// Black pawn attacks down (rank - 1)
		var bFromMask uint64
		if r-1 >= 0 {
			if f > 0 {
				bFromMask |= 1 << ((r-1)*8 + (f - 1))
			}
			if f < 7 {
				bFromMask |= 1 << ((r-1)*8 + (f + 1))
			}
		}
		PawnAttacks[Black][sq] = bFromMask

		// Pawn attacks TO square sq
		// A white pawn on (r-1, f±1) attacks sq (r, f)
		var wToMask uint64
		if r-1 >= 0 {
			if f > 0 {
				wToMask |= 1 << ((r-1)*8 + (f - 1))
			}
			if f < 7 {
				wToMask |= 1 << ((r-1)*8 + (f + 1))
			}
		}
		PawnAttacksTo[White][sq] = wToMask

		// A black pawn on (r+1, f±1) attacks sq (r, f)
		var bToMask uint64
		if r+1 < 8 {
			if f > 0 {
				bToMask |= 1 << ((r+1)*8 + (f - 1))
			}
			if f < 7 {
				bToMask |= 1 << ((r+1)*8 + (f + 1))
			}
		}
		PawnAttacksTo[Black][sq] = bToMask

		// Passed pawn mask: squares ahead in same file and adjacent files
		var wPassed uint64
		for nr := r + 1; nr < 8; nr++ {
			wPassed |= 1 << (nr*8 + f)
			if f > 0 {
				wPassed |= 1 << (nr*8 + f - 1)
			}
			if f < 7 {
				wPassed |= 1 << (nr*8 + f + 1)
			}
		}
		PassedPawnMask[White][sq] = wPassed

		var bPassed uint64
		for nr := 0; nr < r; nr++ {
			bPassed |= 1 << (nr*8 + f)
			if f > 0 {
				bPassed |= 1 << (nr*8 + f - 1)
			}
			if f < 7 {
				bPassed |= 1 << (nr*8 + f + 1)
			}
		}
		PassedPawnMask[Black][sq] = bPassed
	}
}

// AppendKnightMoves and AppendKingMoves generate targets from the
// precomputed attack tables above instead of walking an offset list and
// probing the padded cell array square by square.
//
// The tables have existed in this file since before move generation used
// them: AppendStepMoves walked offsets, and each step cost a bounds check
// against the padding plus a byte load and a colour decode. A table lookup
// masked with the mover's own pieces answers the same question with one AND
// and then one bit-scan per target actually produced.
//
// The targets come out in square-index order rather than offset order, so
// the *set* is identical but the *sequence* is not. Perft is unaffected,
// since it counts; alpha-beta is order sensitive, so node counts will move
// and only Elo can say whether that mattered.
func (b *Board) AppendKnightMoves(dst []Sq, sq Sq, color Color) []Sq {
	return appendFromBitboard(dst, KnightAttacks[squareIndex(sq)]&^b.colorBB[color])
}

func (b *Board) AppendKingMoves(dst []Sq, sq Sq, color Color) []Sq {
	return appendFromBitboard(dst, KingAttacks[squareIndex(sq)]&^b.colorBB[color])
}

// appendFromBitboard walks the set bits lowest first, clearing each with
// the standard x&(x-1) so the loop runs once per target rather than once
// per square.
func appendFromBitboard(dst []Sq, bb uint64) []Sq {
	for bb != 0 {
		i := bits.TrailingZeros64(bb)
		bb &= bb - 1
		dst = append(dst, squareFromIndex(uint8(i)))
	}
	return dst
}

// Ray attacks for the sliding pieces.
//
// rayAttacks[dir][sq] is every square along one compass direction from sq,
// ignoring occupancy. To account for blockers, intersect the ray with the
// occupied squares, find the nearest set bit, and subtract that square's own
// ray: what remains is the ray truncated at the first man, with that man
// included, which is exactly a capture.
//
// This is the classical method rather than magic bitboards. It replaces a
// loop that stepped one square at a time through the padded cell array with
// two table lookups and a bit scan per direction. Magic would be faster
// still and needs a magic-number search and 800 KB of tables; PEXT is not an
// option here at all, being x86 only, and this runs on arm64.
const (
	dirNorth = iota
	dirSouth
	dirEast
	dirWest
	dirNorthEast
	dirNorthWest
	dirSouthEast
	dirSouthWest
	numDirs
)

var rayAttacks [numDirs][64]uint64

// positiveDir says whether a direction's squares have higher indices than
// their origin, which decides whether the nearest blocker is the lowest set
// bit or the highest.
var positiveDir = [numDirs]bool{
	dirNorth: true, dirEast: true, dirNorthEast: true, dirNorthWest: true,
}

func init() {
	deltas := [numDirs][2]int{
		dirNorth:     {0, 1},
		dirSouth:     {0, -1},
		dirEast:      {1, 0},
		dirWest:      {-1, 0},
		dirNorthEast: {1, 1},
		dirNorthWest: {-1, 1},
		dirSouthEast: {1, -1},
		dirSouthWest: {-1, -1},
	}
	for d := 0; d < numDirs; d++ {
		for sq := 0; sq < 64; sq++ {
			f, r := sq%8, sq/8
			var mask uint64
			for {
				f += deltas[d][0]
				r += deltas[d][1]
				if f < 0 || f > 7 || r < 0 || r > 7 {
					break
				}
				mask |= 1 << (r*8 + f)
			}
			rayAttacks[d][sq] = mask
		}
	}
}

// rayFrom is the ray in direction d from sq, truncated at the first
// occupied square and including it.
func rayFrom(d int, sq uint8, occupied uint64) uint64 {
	attacks := rayAttacks[d][sq]
	blockers := attacks & occupied
	if blockers == 0 {
		return attacks
	}
	var first int
	if positiveDir[d] {
		first = bits.TrailingZeros64(blockers)
	} else {
		first = 63 - bits.LeadingZeros64(blockers)
	}
	return attacks &^ rayAttacks[d][first]
}

// BishopAttacks, RookAttacks and QueenAttacks are the squares each piece
// bears on, blockers included and own pieces not yet removed.
func BishopAttacks(sq uint8, occupied uint64) uint64 {
	return rayFrom(dirNorthEast, sq, occupied) | rayFrom(dirNorthWest, sq, occupied) |
		rayFrom(dirSouthEast, sq, occupied) | rayFrom(dirSouthWest, sq, occupied)
}

func RookAttacks(sq uint8, occupied uint64) uint64 {
	return rayFrom(dirNorth, sq, occupied) | rayFrom(dirSouth, sq, occupied) |
		rayFrom(dirEast, sq, occupied) | rayFrom(dirWest, sq, occupied)
}

func (b *Board) occupiedBB() uint64 { return b.colorBB[White] | b.colorBB[Black] }

// Move generation deliberately still walks the rays through the cell array.
// Measured here, replacing it was not worth it either way round:
//
// Emitting the union of the four rays in square-index order is about 1.7x
// faster per queen and 7% faster over a depth-5 search, but it reorders the
// move list, and alpha-beta prunes by a move's position in that list. Deep
// late move pruning went from cutting 11% of nodes on the four correctness
// positions to adding 13%, so the speed buys a worse-ordered search and the
// net would need thousands of games to read.
//
// Emitting direction by direction instead keeps the order and the tree
// identical, but then the per-direction bit-scan loops cost as much as the
// walk they replace: 19.1-19.5 ns per queen against the walk's 19.3-20.1.
//
// So the tables earn their place in IsAttackedBy, which returns a bool and
// has no order to preserve, and nowhere in move generation.
