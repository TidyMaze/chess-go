// Package moves implements pseudo-legal move generation, check detection,
// and pin detection for the chess engine.
package moves

import (
	"math/bits"

	"chess/board"
)

// Indexed by Color, not keyed by it: read in every move-generation call.
var startRank = [2]int{board.White: 1, board.Black: 6}
var direction = [2]int{board.White: 1, board.Black: -1}

var knightOffsets = [8][2]int{{1, 2}, {1, -2}, {-1, 2}, {-1, -2}, {2, 1}, {2, -1}, {-2, 1}, {-2, -1}}
var kingOffsets = [8][2]int{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}
var bishopDirs = [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
var rookDirs = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
var queenDirs = [8][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}}

func stepMoves(dst []board.Sq, b *board.Board, sq board.Sq, color board.Color, offsets [][2]int) []board.Sq {
	return b.AppendStepMoves(dst, sq, color, offsets)
}

func slideMoves(dst []board.Sq, b *board.Board, sq board.Sq, color board.Color, dirs [][2]int) []board.Sq {
	return b.AppendSlideMoves(dst, sq, color, dirs)
}

func pawnMoves(dst []board.Sq, b *board.Board, sq board.Sq, color board.Color) []board.Sq {
	return b.AppendPawnMoves(dst, sq, color)
}

// Precomputed once: these were being rebuilt on every LegalTargets call,
// allocating a fresh slice per move-generation call purely to convert an
// array to a slice.
var (
	knightOffsetSlice = knightOffsets[:]
	kingOffsetSlice   = kingOffsets[:]
	bishopDirSlice    = bishopDirs[:]
	rookDirSlice      = rookDirs[:]
	queenDirSlice     = append(append([][2]int{}, bishopDirs[:]...), rookDirs[:]...)
)

// AppendLegalTargets appends pseudo-legal target squares (not yet filtered
// for leaving your own king in check -- that's game.AllLegalMoves's job)
// to dst. Taking a destination buffer lets the caller reuse one slice
// across every piece instead of allocating a fresh one per piece, which
// was the largest remaining allocation source in the search.
func AppendLegalTargets(dst []board.Sq, b *board.Board, sq board.Sq, color board.Color, pt board.PieceType) []board.Sq {
	switch pt {
	case board.Pawn:
		return pawnMoves(dst, b, sq, color)
	case board.Knight:
		return b.AppendKnightMoves(dst, sq, color)
	case board.King:
		return castlingMoves(b.AppendKingMoves(dst, sq, color), b, sq, color)
	case board.Bishop:
		return slideMoves(dst, b, sq, color, bishopDirSlice)
	case board.Rook:
		return slideMoves(dst, b, sq, color, rookDirSlice)
	case board.Queen:
		return slideMoves(dst, b, sq, color, queenDirSlice)
	}
	panic("unknown piece type")
}

// AppendQuiescenceTargets appends what quiescence searches for a piece at
// sq: its captures, a pawn's en passant capture and its push to the last
// rank. Quiet moves are never generated. Steppers and pawns come out in
// square order and sliders one direction at a time in their direction
// order, the order the walks produced, because move ordering breaks ties
// on the generated index.
func AppendQuiescenceTargets(dst []board.Sq, b *board.Board, sq board.Sq, color board.Color, pt board.PieceType) []board.Sq {
	i := uint8(sq.Rank*8+sq.File) & 63
	enemy := b.ColorBitboard(color.Other())
	switch pt {
	case board.Pawn:
		capturable := enemy
		if ep, ok := b.EPSquare(); ok {
			capturable |= 1 << (ep.Rank*8 + ep.File)
		}
		targets := board.PawnAttacks[color][i] & capturable
		occupied := enemy | b.ColorBitboard(color)
		if color == board.White && i >= 48 && i < 56 {
			targets |= 1 << (i + 8) &^ occupied
		} else if color == board.Black && i >= 8 && i < 16 {
			targets |= 1 << (i - 8) &^ occupied
		}
		return appendBits(dst, targets)
	case board.Knight:
		return appendBits(dst, board.KnightAttacks[i]&enemy)
	case board.King:
		return appendBits(dst, board.KingAttacks[i]&enemy)
	case board.Bishop:
		if bishopLines[i]&enemy == 0 {
			return dst
		}
		return appendSlideCaptures(dst, b, i, enemy, 0, 4)
	case board.Rook:
		if rookLines[i]&enemy == 0 {
			return dst
		}
		return appendSlideCaptures(dst, b, i, enemy, 4, 8)
	case board.Queen:
		if (rookLines[i]|bishopLines[i])&enemy == 0 {
			return dst
		}
		return appendSlideCaptures(dst, b, i, enemy, 0, 8)
	}
	panic("unknown piece type")
}

// positiveRay says whether queenDirs[k] runs towards higher square
// indices, so the nearest man on that ray is the lowest set bit.
var positiveRay = func() (t [8]bool) {
	for k, d := range queenDirs {
		t[k] = d[1]*8+d[0] > 0
	}
	return t
}()

// appendSlideCaptures appends, for each direction queenDirs[first:last] in
// turn, the first man on the ray from i when it is an enemy.
func appendSlideCaptures(dst []board.Sq, b *board.Board, i uint8, enemy uint64, first, last int) []board.Sq {
	occupied := enemy | b.ColorBitboard(board.White) | b.ColorBitboard(board.Black)
	for k := first; k < last; k++ {
		blockers := sliderRays[k][i] & occupied
		if blockers == 0 {
			continue
		}
		var s int
		if positiveRay[k] {
			s = bits.TrailingZeros64(blockers)
		} else {
			s = 63 - bits.LeadingZeros64(blockers)
		}
		if enemy&(1<<s) != 0 {
			dst = append(dst, board.Sq{File: int8(s & 7), Rank: int8(s >> 3)})
		}
	}
	return dst
}

// appendBits appends the squares of bb lowest first.
func appendBits(dst []board.Sq, bb uint64) []board.Sq {
	for bb != 0 {
		s := bits.TrailingZeros64(bb)
		bb &= bb - 1
		dst = append(dst, board.Sq{File: int8(s & 7), Rank: int8(s >> 3)})
	}
	return dst
}

// LegalTargets is the allocating convenience form, for tests and callers
// outside the search hot path.
func LegalTargets(b *board.Board, sq board.Sq, color board.Color, pt board.PieceType) []board.Sq {
	return AppendLegalTargets(nil, b, sq, color, pt)
}

// IsInCheck probes outward from the king (knight/king offsets, rook/bishop
// ray-scans, pawn-attack squares) instead of generating every enemy move:
// O(1) piece-type checks instead of O(enemy pieces).
// IsInCheck probes outward from the king: fast O(1) cell reads on the Board.
func IsInCheck(b *board.Board, color board.Color) bool {
	return b.IsInCheck(color)
}

// IsAttackedBy probes outward from a square to see whether `by` attacks it.
func IsAttackedBy(b *board.Board, sq board.Sq, by board.Color) bool {
	return b.IsAttackedBy(sq, by)
}

// PinnedSquares returns the set of color's own squares that are pinned to
// its king: moving that piece could expose check, so its legality needs
// the expensive per-move self-check test. Computed once per position
// instead of once per candidate move -- this is what makes legal move
// generation fast.
type pinRay struct {
	d       [2]int
	slider  board.PieceType
	slider2 board.PieceType
}

// Precomputed once: this was rebuilt (with two slice allocations per
// direction) on every call, and PinnedSquares runs once per search node.
var pinRays = func() [8]pinRay {
	var out [8]pinRay
	i := 0
	for _, d := range rookDirs {
		out[i] = pinRay{d, board.Rook, board.Queen}
		i++
	}
	for _, d := range bishopDirs {
		out[i] = pinRay{d, board.Bishop, board.Queen}
		i++
	}
	return out
}()

// PinnedSet is a 64-bit bitset of pinned squares: checking membership
// is a single shift and bitwise AND instruction, and Len is popcount.
type PinnedSet uint64

func (p PinnedSet) Has(s board.Sq) bool {
	return (p & (1 << (s.Rank*8 + s.File))) != 0
}

func (p PinnedSet) Len() int {
	return bits.OnesCount64(uint64(p))
}

func (p *PinnedSet) add(s board.Sq) {
	*p |= 1 << (s.Rank*8 + s.File)
}

func PinnedSquares(b *board.Board, color board.Color) PinnedSet {
	// A pin needs an enemy slider on the king's line, blockers aside, and
	// most positions have none. For each one that is there, the squares
	// between it and the king hold the pinned man when they hold exactly
	// one man and it is ours: an x-ray instead of walking the ray cell by
	// cell.
	king := b.KingSquare(color)
	k := uint8(king.Rank*8+king.File) & 63
	enemy := color.Other()
	queens := b.PieceBitboard(enemy, board.Queen)
	snipers := rookLines[k]&(b.PieceBitboard(enemy, board.Rook)|queens) |
		bishopLines[k]&(b.PieceBitboard(enemy, board.Bishop)|queens)
	if snipers == 0 {
		return 0
	}
	own := b.ColorBitboard(color)
	occupied := own | b.ColorBitboard(enemy)
	var pinned uint64
	for snipers != 0 {
		s := bits.TrailingZeros64(snipers)
		snipers &= snipers - 1
		if blockers := betweenBB[k][s&63] & occupied; blockers&(blockers-1) == 0 && blockers&own != 0 {
			pinned |= blockers
		}
	}
	return PinnedSet(pinned)
}

// PinnedSquaresUnfiltered is the unconditional eight-ray scan, kept as the
// oracle PinnedSquares is tested against. It lives here because the test
// needs real games to meet a pin, and the game package imports this one.
func PinnedSquaresUnfiltered(b *board.Board, color board.Color) PinnedSet {
	return pinnedByWalk(b, color, b.KingSquare(color), true, true)
}

// pinnedByWalk walks the rays whose slider kind is present. With both true
// it is the full eight-ray scan.
func pinnedByWalk(b *board.Board, color board.Color, king board.Sq, rooks, bishops bool) PinnedSet {
	var pinned PinnedSet
	for _, ry := range pinRays {
		if (ry.slider == board.Rook && !rooks) || (ry.slider == board.Bishop && !bishops) {
			continue
		}
		if ownSq, ok := b.FindPinnedPiece(king, ry.d[0], ry.d[1], color, ry.slider, ry.slider2); ok {
			pinned.add(ownSq)
		}
	}
	return pinned
}

// betweenBB[a][b] holds the squares strictly between a and b and lineBB[a][b]
// the whole line through both, edge to edge, when they share a rank, file
// or diagonal. Both are zero when they do not.
var betweenBB, lineBB = func() (between, line [64][64]uint64) {
	for k, d := range queenDirs {
		back := 0
		for j, e := range queenDirs {
			if e[0] == -d[0] && e[1] == -d[1] {
				back = j
			}
		}
		for a := 0; a < 64; a++ {
			full := sliderRays[k][a] | sliderRays[back][a] | 1<<a
			var path uint64
			for f, r := a%8+d[0], a/8+d[1]; f >= 0 && f < 8 && r >= 0 && r < 8; f, r = f+d[0], r+d[1] {
				between[a][r*8+f] = path
				line[a][r*8+f] = full
				path |= 1 << (r*8 + f)
			}
		}
	}
	return between, line
}()

// rookLines and bishopLines are the empty-board rays of a rook and a
// bishop: where an enemy slider has to stand to pin or check at all.
var rookLines, bishopLines = func() (rook, bishop [64]uint64) {
	for i := 0; i < 64; i++ {
		for k := 0; k < 4; k++ {
			bishop[i] |= sliderRays[k][i]
			rook[i] |= sliderRays[k+4][i]
		}
	}
	return rook, bishop
}()

// Between is the set of squares strictly between a and b, or zero when
// they are not on one rank, file or diagonal.
func Between(a, b uint8) uint64 { return betweenBB[a&63][b&63] }

// Line is the whole rank, file or diagonal through a and b, or zero when
// there is none. A pinned man stays legal exactly while it stays on the
// line through its king and itself.
func Line(a, b uint8) uint64 { return lineBB[a&63][b&63] }

// AttackedWith reports whether by attacks sq when the board's men stand
// where they are but the occupancy that blocks sliders is occupied. With
// the mover's king lifted off it answers whether a king step is legal
// without playing it: the king cannot hide from a slider behind the
// square it is leaving.
func AttackedWith(b *board.Board, sq uint8, by board.Color, occupied uint64) bool {
	sq &= 63
	if board.PawnAttacksTo[by][sq]&b.PieceBitboard(by, board.Pawn) != 0 ||
		board.KnightAttacks[sq]&b.PieceBitboard(by, board.Knight) != 0 {
		return true
	}
	k := b.KingSquare(by)
	if board.KingAttacks[sq]&(1<<(k.Rank*8+k.File)) != 0 {
		return true
	}
	queens := b.PieceBitboard(by, board.Queen)
	if rq := b.PieceBitboard(by, board.Rook) | queens; rq&rookLines[sq] != 0 && board.RookAttacks(sq, occupied)&rq != 0 {
		return true
	}
	bq := b.PieceBitboard(by, board.Bishop) | queens
	return bq&bishopLines[sq] != 0 && board.BishopAttacks(sq, occupied)&bq != 0
}

// castlingMoves appends the castling destinations available to a king.
//
// The rule has four conditions and each one is a separate way to produce
// an illegal move, so they are checked explicitly rather than folded
// together: the right must still exist, the squares between king and
// rook must be empty, and the king may not start in check, pass through
// an attacked square, or land on one.
//
// The rook's path is deliberately not checked for attacks. Only the king
// is restricted that way, so queenside castling stays legal when b1 is
// attacked even though the rook crosses it.
func castlingMoves(dst []board.Sq, b *board.Board, sq board.Sq, color board.Color) []board.Sq {
	rank := int8(0)
	kingSide, queenSide := board.WhiteKingSide, board.WhiteQueenSide
	if color == board.Black {
		rank, kingSide, queenSide = 7, board.BlackKingSide, board.BlackQueenSide
	}
	// A king that has been moved off e1/e8 has already lost its rights,
	// but the position may also have been set up that way.
	if sq.Rank != rank || sq.File != 4 {
		return dst
	}
	rights := b.Castle()
	if rights&(kingSide|queenSide) == 0 {
		return dst
	}
	enemy := color.Other()
	if IsAttackedBy(b, sq, enemy) {
		return dst // may not castle out of check
	}

	empty := func(files ...int8) bool {
		for _, f := range files {
			if _, occupied := b.PieceAt(board.Sq{File: f, Rank: rank}); occupied {
				return false
			}
		}
		return true
	}
	safe := func(files ...int8) bool {
		for _, f := range files {
			if IsAttackedBy(b, board.Sq{File: f, Rank: rank}, enemy) {
				return false
			}
		}
		return true
	}
	rookAt := func(file int8) bool {
		p, ok := b.PieceAt(board.Sq{File: file, Rank: rank})
		return ok && p.Type == board.Rook && p.Color == color
	}

	if rights&kingSide != 0 && rookAt(7) && empty(5, 6) && safe(5, 6) {
		dst = append(dst, board.Sq{File: 6, Rank: rank})
	}
	if rights&queenSide != 0 && rookAt(0) && empty(1, 2, 3) && safe(2, 3) {
		dst = append(dst, board.Sq{File: 2, Rank: rank})
	}
	return dst
}

// sliderRays[k][i] is every square from square index i to the board edge
// in direction queenDirs[k], blockers ignored. Only the direction matters:
// it tells which of two attackers the ray walk would have met first.
var sliderRays = func() (t [8][64]uint64) {
	for k, d := range queenDirs {
		for i := 0; i < 64; i++ {
			for f, r := i%8+d[0], i/8+d[1]; f >= 0 && f < 8 && r >= 0 && r < 8; f, r = f+d[0], r+d[1] {
				t[k][i] |= 1 << (r*8 + f)
			}
		}
	}
	return t
}()

// sliderAttacker is AttackerOfType for a bishop, rook or queen, on
// bitboards instead of a square-by-square walk. Each ray is cut at its
// first man, so a ray holds at most one attacker. When two pieces of the
// type attack sq it returns the one on the earlier direction of
// queenDirs, the one the walk met first: exchange evaluation takes that
// piece first, and a different pick changes SEE values and the tree.
func sliderAttacker(b *board.Board, sq board.Sq, by board.Color, typ board.PieceType) (board.Sq, bool) {
	own := b.PieceBitboard(by, typ)
	if own == 0 {
		return board.Sq{}, false
	}
	i := uint8(sq.Rank*8 + sq.File)
	occupied := b.ColorBitboard(board.White) | b.ColorBitboard(board.Black)
	var reach uint64
	first, last := 0, 8 // queenDirs: the four diagonals, then the four lines
	if typ != board.Rook {
		reach = board.BishopAttacks(i, occupied)
	} else {
		first = 4
	}
	if typ != board.Bishop {
		reach |= board.RookAttacks(i, occupied)
	} else {
		last = 4
	}
	attackers := reach & own
	if attackers == 0 {
		return board.Sq{}, false
	}
	if attackers&(attackers-1) != 0 {
		for k := first; k < last; k++ {
			if a := sliderRays[k][i] & attackers; a != 0 {
				attackers = a
				break
			}
		}
	}
	idx := bits.TrailingZeros64(attackers)
	return board.Sq{File: int8(idx % 8), Rank: int8(idx / 8)}, true
}

// AttackerOfType finds one piece of colour by and type typ that attacks
// sq, for static exchange evaluation, which wants attackers cheapest
// first. Sliders are found along their rays, the first piece met on a ray
// being the only one that attacks; a queen is reported only when asked
// for a queen, so a caller scanning for bishops does not take a queen.
func AttackerOfType(b *board.Board, sq board.Sq, by board.Color, typ board.PieceType) (board.Sq, bool) {
	switch typ {
	case board.Pawn:
		if sq.File >= 0 && sq.File < 8 && sq.Rank >= 0 && sq.Rank < 8 {
			sqIdx := sq.Rank*8 + sq.File
			attackers := board.PawnAttacksTo[by][sqIdx] & b.PieceBitboard(by, board.Pawn)
			if attackers != 0 {
				idx := bits.TrailingZeros64(attackers)
				return board.Sq{File: int8(idx % 8), Rank: int8(idx / 8)}, true
			}
		}
	case board.Knight:
		if sq.File >= 0 && sq.File < 8 && sq.Rank >= 0 && sq.Rank < 8 {
			sqIdx := sq.Rank*8 + sq.File
			attackers := board.KnightAttacks[sqIdx] & b.PieceBitboard(by, board.Knight)
			if attackers != 0 {
				idx := bits.TrailingZeros64(attackers)
				return board.Sq{File: int8(idx % 8), Rank: int8(idx / 8)}, true
			}
		}
	case board.King:
		if sq.File >= 0 && sq.File < 8 && sq.Rank >= 0 && sq.Rank < 8 {
			sqIdx := sq.Rank*8 + sq.File
			k := b.KingSquare(by)
			kIdx := k.Rank*8 + k.File
			if board.KingAttacks[sqIdx]&(1<<kIdx) != 0 {
				return k, true
			}
		}
	case board.Bishop, board.Rook, board.Queen:
		return sliderAttacker(b, sq, by, typ)
	}
	return board.Sq{}, false
}
