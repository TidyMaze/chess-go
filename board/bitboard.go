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

func (b *Board) AppendKnightCaptures(dst []Sq, sq Sq, color Color) []Sq {
	return appendFromBitboard(dst, KnightAttacks[squareIndex(sq)]&b.colorBB[color.Other()])
}

func (b *Board) AppendKingMoves(dst []Sq, sq Sq, color Color) []Sq {
	return appendFromBitboard(dst, KingAttacks[squareIndex(sq)]&^b.colorBB[color])
}

func (b *Board) AppendKingCaptures(dst []Sq, sq Sq, color Color) []Sq {
	return appendFromBitboard(dst, KingAttacks[squareIndex(sq)]&b.colorBB[color.Other()])
}

func (b *Board) AppendPawnMoves(dst []Sq, sq Sq, color Color) []Sq {
	sqIdx := squareIndex(sq)
	var targets uint64
	occ := b.occupiedBB()

	if color == White {
		// Single push
		single := uint64(1) << (sqIdx + 8)
		if sqIdx < 56 && (single&occ) == 0 {
			targets |= single
			// Double push
			if sq.Rank == 1 {
				double := uint64(1) << (sqIdx + 16)
				if (double & occ) == 0 {
					targets |= double
				}
			}
		}
	} else {
		// Single push
		if sqIdx >= 8 {
			single := uint64(1) << (sqIdx - 8)
			if (single & occ) == 0 {
				targets |= single
				// Double push
				if sq.Rank == 6 {
					double := uint64(1) << (sqIdx - 16)
					if (double & occ) == 0 {
						targets |= double
					}
				}
			}
		}
	}

	// Captures & En Passant
	capturable := b.colorBB[color.Other()]
	if b.epSquare != noEP {
		capturable |= uint64(1) << (b.epSquare & 63)
	}
	targets |= PawnAttacks[color][sqIdx] & capturable

	return appendFromBitboard(dst, targets)
}

func (b *Board) AppendBishopMoves(dst []Sq, sq Sq, color Color) []Sq {
	sqIdx := squareIndex(sq)
	targets := BishopAttacks(sqIdx, b.occupiedBB()) &^ b.colorBB[color]
	return appendFromBitboard(dst, targets)
}

func (b *Board) AppendRookMoves(dst []Sq, sq Sq, color Color) []Sq {
	sqIdx := squareIndex(sq)
	targets := RookAttacks(sqIdx, b.occupiedBB()) &^ b.colorBB[color]
	return appendFromBitboard(dst, targets)
}

func (b *Board) AppendQueenMoves(dst []Sq, sq Sq, color Color) []Sq {
	sqIdx := squareIndex(sq)
	occ := b.occupiedBB()
	targets := (RookAttacks(sqIdx, occ) | BishopAttacks(sqIdx, occ)) &^ b.colorBB[color]
	return appendFromBitboard(dst, targets)
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
// ignoring occupancy. The slider attack sets below are computed a line at a
// time (see lineAttacks); the rays split such a set back into directions,
// which is what keeps a capture list in its walk order.
//
// Neither magic bitboards nor PEXT: magic needs a magic-number search and
// 800 KB of tables, and PEXT is x86 only while this runs on arm64.
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

// dirByDelta maps a unit step (df, dr), stored at (dr+1)*3 + df+1, to its
// direction. The centre entry, no step at all, is not a direction.
var dirByDelta = [9]uint8{
	dirSouthWest, dirSouth, dirSouthEast,
	dirWest, 0, dirEast,
	dirNorthWest, dirNorth, dirNorthEast,
}

// dirOf is the direction of a unit step d, as the move generator's
// direction lists write it ({df, dr}).
func dirOf(d [2]int) int {
	return int(dirByDelta[(d[1]+1)*3+d[0]+1])
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
	for sq := range lineMasks {
		lineMasks[sq] = sliderLines{
			file: rayAttacks[dirNorth][sq] | rayAttacks[dirSouth][sq],
			rank: rayAttacks[dirEast][sq] | rayAttacks[dirWest][sq],
			diag: rayAttacks[dirNorthEast][sq] | rayAttacks[dirSouthWest][sq],
			anti: rayAttacks[dirNorthWest][sq] | rayAttacks[dirSouthEast][sq],
		}
	}
}

// Hyperbola quintessence: the attacks along one line through sq, both
// directions at once and without a branch.
//
// With o the occupied squares on the line, o-2s borrows from the square
// above sq up to the nearest blocker above, flipping exactly those bits;
// doing the same on the bit-reversed board (bits.Reverse64 is one RBIT on
// arm64) flips the squares down to the nearest blocker below. XOR the two
// and the untouched blockers cancel, leaving both rays with their blockers
// included. Reversing all 64 bits rather than byte-swapping works for ranks
// too, so one formula covers the four lines.
//
// It replaces a ray per direction cut by a data-dependent bit scan, eight
// unpredictable branches per queen.
type sliderLines struct{ file, rank, diag, anti uint64 }

// lineMasks holds the four lines through each square, the square itself
// left out. Filled in init, after rayAttacks, which it is built from.
var lineMasks [64]sliderLines

func lineAttacks(sq uint8, occ, mask uint64) uint64 {
	o := occ & mask
	up := o - uint64(2)<<(sq&63)
	down := bits.Reverse64(bits.Reverse64(o) - uint64(2)<<((63-sq)&63))
	return (up ^ down) & mask
}

// BishopAttacks, RookAttacks and QueenAttacks are the squares each piece
// bears on, blockers included and own pieces not yet removed.
func BishopAttacks(sq uint8, occupied uint64) uint64 {
	l := &lineMasks[sq&63]
	return lineAttacks(sq, occupied, l.diag) | lineAttacks(sq, occupied, l.anti)
}

func RookAttacks(sq uint8, occupied uint64) uint64 {
	l := &lineMasks[sq&63]
	return lineAttacks(sq, occupied, l.file) | lineAttacks(sq, occupied, l.rank)
}

func (b *Board) occupiedBB() uint64 { return b.colorBB[White] | b.colorBB[Black] }

// Quiet move generation (AppendSlideMoves) deliberately still walks the
// rays through the cell array. Measured here, replacing it was not worth it
// either way round:
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
// has no order to preserve, and in AppendSlideCaptures, where there is at
// most one target per direction and usually none at all, so one attack-set
// test replaces the whole walk.

// TargetBitboard is the pseudo-legal target set of a knight, bishop, rook
// or queen on sq, own pieces removed: the same squares AppendStepMoves and
// AppendSlideMoves would append, as a set rather than a sequence. That is
// enough for anything that only counts targets, and a count is where the
// tables beat the walk without reordering a single move. Pawns and kings
// return false: their targets depend on more than occupancy (double
// pushes, en passant, castling) and stay on the walk.
func (b *Board) TargetBitboard(sq Sq, color Color, pt PieceType) (uint64, bool) {
	i := squareIndex(sq)
	own := b.colorBB[color]
	switch pt {
	case Knight:
		return KnightAttacks[i] &^ own, true
	case Bishop:
		return BishopAttacks(i, b.occupiedBB()) &^ own, true
	case Rook:
		return RookAttacks(i, b.occupiedBB()) &^ own, true
	case Queen:
		occ := b.occupiedBB()
		return (BishopAttacks(i, occ) | RookAttacks(i, occ)) &^ own, true
	}
	return 0, false
}

// AlignedSliders says whether any enemy rook or queen shares a rank or
// file with king, and whether any enemy bishop or queen shares a diagonal,
// blockers ignored. A pin needs exactly that alignment, so when both are
// false there is no pin to find and the eight ray walks can be skipped;
// in most middlegame positions that is the common case.
func (b *Board) AlignedSliders(king Sq, enemy Color) (rooks, bishops bool) {
	i := squareIndex(king)
	queens := b.pieces[enemy][Queen]
	rooks = RookAttacks(i, 0)&(b.pieces[enemy][Rook]|queens) != 0
	bishops = BishopAttacks(i, 0)&(b.pieces[enemy][Bishop]|queens) != 0
	return rooks, bishops
}

// KingZoneHits returns the number of squares in zoneTarget attacked by a piece
// of type pt on square sqIdx. zoneTarget is the king zone with the attacker's
// own pieces already removed.
func (b *Board) KingZoneHits(sqIdx uint8, pt PieceType, occ, zoneTarget uint64) int {
	switch pt {
	case Knight:
		return bits.OnesCount64(KnightAttacks[sqIdx&63] & zoneTarget)
	case Bishop:
		l := &lineMasks[sqIdx&63]
		hits := 0
		if l.diag&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(sqIdx, occ, l.diag) & zoneTarget)
		}
		if l.anti&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(sqIdx, occ, l.anti) & zoneTarget)
		}
		return hits
	case Rook:
		l := &lineMasks[sqIdx&63]
		hits := 0
		if l.file&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(sqIdx, occ, l.file) & zoneTarget)
		}
		if l.rank&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(sqIdx, occ, l.rank) & zoneTarget)
		}
		return hits
	case Queen:
		l := &lineMasks[sqIdx&63]
		hits := 0
		if l.diag&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(sqIdx, occ, l.diag) & zoneTarget)
		}
		if l.anti&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(sqIdx, occ, l.anti) & zoneTarget)
		}
		if l.file&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(sqIdx, occ, l.file) & zoneTarget)
		}
		if l.rank&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(sqIdx, occ, l.rank) & zoneTarget)
		}
		return hits
	}
	return 0
}

// KingZoneAttacks counts enemy attackers and weighted hits into zone around king.
func (b *Board) KingZoneAttacks(enemy Color, zone uint64) (int, float64) {
	zoneTarget := zone &^ b.colorBB[enemy]
	if zoneTarget == 0 {
		return 0, 0
	}
	occ := b.occupiedBB()
	attackers := 0
	weightSum := 0.0

	// Knights (weight 2)
	for bb := b.pieces[enemy][Knight]; bb != 0; bb &= bb - 1 {
		hits := bits.OnesCount64(KnightAttacks[bits.TrailingZeros64(bb)&63] & zoneTarget)
		if hits > 0 {
			attackers++
			weightSum += 2.0 * float64(hits)
		}
	}
	// Bishops (weight 2)
	for bb := b.pieces[enemy][Bishop]; bb != 0; bb &= bb - 1 {
		sqIdx := bits.TrailingZeros64(bb) & 63
		l := &lineMasks[sqIdx]
		if (l.diag|l.anti)&zoneTarget == 0 {
			continue
		}
		hits := 0
		if l.diag&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(uint8(sqIdx), occ, l.diag) & zoneTarget)
		}
		if l.anti&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(uint8(sqIdx), occ, l.anti) & zoneTarget)
		}
		if hits > 0 {
			attackers++
			weightSum += 2.0 * float64(hits)
		}
	}
	// Rooks (weight 3)
	for bb := b.pieces[enemy][Rook]; bb != 0; bb &= bb - 1 {
		sqIdx := bits.TrailingZeros64(bb) & 63
		l := &lineMasks[sqIdx]
		if (l.file|l.rank)&zoneTarget == 0 {
			continue
		}
		hits := 0
		if l.file&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(uint8(sqIdx), occ, l.file) & zoneTarget)
		}
		if l.rank&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(uint8(sqIdx), occ, l.rank) & zoneTarget)
		}
		if hits > 0 {
			attackers++
			weightSum += 3.0 * float64(hits)
		}
	}
	// Queens (weight 5)
	for bb := b.pieces[enemy][Queen]; bb != 0; bb &= bb - 1 {
		sqIdx := bits.TrailingZeros64(bb) & 63
		l := &lineMasks[sqIdx]
		if (l.diag|l.anti|l.file|l.rank)&zoneTarget == 0 {
			continue
		}
		hits := 0
		if l.diag&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(uint8(sqIdx), occ, l.diag) & zoneTarget)
		}
		if l.anti&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(uint8(sqIdx), occ, l.anti) & zoneTarget)
		}
		if l.file&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(uint8(sqIdx), occ, l.file) & zoneTarget)
		}
		if l.rank&zoneTarget != 0 {
			hits += bits.OnesCount64(lineAttacks(uint8(sqIdx), occ, l.rank) & zoneTarget)
		}
		if hits > 0 {
			attackers++
			weightSum += 5.0 * float64(hits)
		}
	}

	return attackers, weightSum
}


