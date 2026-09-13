package board

// Bitboard constants and precomputed attack/structure tables.
// Square mapping: Little-Endian Rank-File (sq = rank*8 + file, 0=a1, 63=h8).

var (
	KnightAttacks      [64]uint64
	KingAttacks        [64]uint64
	PawnAttacks        [2][64]uint64 // Attacks FROM square sq for color
	PawnAttacksTo      [2][64]uint64 // Attacks TO square sq from pawns of color
	FileMask           [8]uint64
	AdjacentFilesMask  [8]uint64
	PassedPawnMask     [2][64]uint64
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
