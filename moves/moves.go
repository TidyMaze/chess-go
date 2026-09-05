// Package moves implements pseudo-legal move generation, check detection,
// and pin detection for the chess engine.
package moves

import "chess/board"

// Indexed by Color, not keyed by it: read in every move-generation call.
var startRank = [2]int{board.White: 1, board.Black: 6}
var direction = [2]int{board.White: 1, board.Black: -1}

var knightOffsets = [8][2]int{{1, 2}, {1, -2}, {-1, 2}, {-1, -2}, {2, 1}, {2, -1}, {-2, 1}, {-2, -1}}
var kingOffsets = [8][2]int{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}
var bishopDirs = [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
var rookDirs = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

func stepMoves(dst []board.Sq, b *board.Board, sq board.Sq, color board.Color, offsets [][2]int) []board.Sq {
	moves := dst
	for _, d := range offsets {
		target := board.Sq{File: sq.File + d[0], Rank: sq.Rank + d[1]}
		if b.CellOffBoard(target) {
			continue
		}
		piece, occupied := b.CellPiece(target)
		if !occupied || piece.Color != color {
			moves = append(moves, target)
		}
	}
	return moves
}

func slideMoves(dst []board.Sq, b *board.Board, sq board.Sq, color board.Color, dirs [][2]int) []board.Sq {
	moves := dst
	for _, d := range dirs {
		f, r := sq.File+d[0], sq.Rank+d[1]
		for {
			target := board.Sq{File: f, Rank: r}
			if b.CellOffBoard(target) {
				break
			}
			piece, occupied := b.CellPiece(target)
			if !occupied {
				moves = append(moves, target)
			} else {
				if piece.Color != color {
					moves = append(moves, target)
				}
				break
			}
			f, r = f+d[0], r+d[1]
		}
	}
	return moves
}

func pawnMoves(dst []board.Sq, b *board.Board, sq board.Sq, color board.Color) []board.Sq {
	dir := direction[color]
	moves := dst

	// The off-board check matters: without it a pawn on the last rank
	// generates a move into the padding (CellPiece reports padding as
	// "not occupied"), silently corrupting the board and crashing several
	// plies later when a probe from there runs past the array.
	oneAhead := board.Sq{File: sq.File, Rank: sq.Rank + dir}
	if _, occupied := b.CellPiece(oneAhead); !occupied && !b.CellOffBoard(oneAhead) {
		moves = append(moves, oneAhead)
		if sq.Rank == startRank[color] {
			twoAhead := board.Sq{File: sq.File, Rank: sq.Rank + 2*dir}
			if _, occ := b.CellPiece(twoAhead); !occ {
				moves = append(moves, twoAhead)
			}
		}
	}

	for _, df := range [2]int{-1, 1} {
		target := board.Sq{File: sq.File + df, Rank: sq.Rank + dir}
		if b.CellOffBoard(target) {
			continue
		}
		if piece, occupied := b.CellPiece(target); occupied && piece.Color != color {
			moves = append(moves, target)
			continue
		}
		// En passant: the target square is empty, but a pawn that has
		// just stepped two squares can be taken as if it had stepped one.
		if ep, ok := b.EPSquare(); ok && ep == target {
			moves = append(moves, target)
		}
	}
	return moves
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
		return stepMoves(dst, b, sq, color, knightOffsetSlice)
	case board.King:
		return castlingMoves(stepMoves(dst, b, sq, color, kingOffsetSlice), b, sq, color)
	case board.Bishop:
		return slideMoves(dst, b, sq, color, bishopDirSlice)
	case board.Rook:
		return slideMoves(dst, b, sq, color, rookDirSlice)
	case board.Queen:
		return slideMoves(dst, b, sq, color, queenDirSlice)
	}
	panic("unknown piece type")
}

// LegalTargets is the allocating convenience form, for tests and callers
// outside the search hot path.
func LegalTargets(b *board.Board, sq board.Sq, color board.Color, pt board.PieceType) []board.Sq {
	return AppendLegalTargets(nil, b, sq, color, pt)
}

// IsInCheck probes outward from the king (knight/king offsets, rook/bishop
// ray-scans, pawn-attack squares) instead of generating every enemy move:
// O(1) piece-type checks instead of O(enemy pieces).
func IsInCheck(b *board.Board, color board.Color) bool {
	king := b.KingSquare(color)
	enemy := color.Other()

	for _, d := range knightOffsets {
		target := board.Sq{File: king.File + d[0], Rank: king.Rank + d[1]}
		if p, ok := b.CellPiece(target); ok && p.Color == enemy && p.Type == board.Knight {
			return true
		}
	}
	for _, d := range kingOffsets {
		target := board.Sq{File: king.File + d[0], Rank: king.Rank + d[1]}
		if p, ok := b.CellPiece(target); ok && p.Color == enemy && p.Type == board.King {
			return true
		}
	}
	for _, d := range rookDirs {
		if hitsSlider(b, king, d, enemy, board.Rook, board.Queen) {
			return true
		}
	}
	for _, d := range bishopDirs {
		if hitsSlider(b, king, d, enemy, board.Bishop, board.Queen) {
			return true
		}
	}
	enemyDir := direction[enemy]
	for _, df := range [2]int{-1, 1} {
		target := board.Sq{File: king.File + df, Rank: king.Rank - enemyDir}
		if p, ok := b.CellPiece(target); ok && p.Color == enemy && p.Type == board.Pawn {
			return true
		}
	}
	return false
}

func hitsSlider(b *board.Board, from board.Sq, d [2]int, enemy board.Color, types ...board.PieceType) bool {
	f, r := from.File+d[0], from.Rank+d[1]
	for {
		target := board.Sq{File: f, Rank: r}
		if b.CellOffBoard(target) {
			return false
		}
		p, occupied := b.CellPiece(target)
		if occupied {
			if p.Color != enemy {
				return false
			}
			for _, t := range types {
				if p.Type == t {
					return true
				}
			}
			return false
		}
		f, r = f+d[0], r+d[1]
	}
}

// IsAttackedBy probes outward from a square to see whether `by` attacks
// it. Same technique as IsInCheck, applied to any square: O(1) piece-type
// probes rather than generating every one of that side's moves and
// checking their destinations, which is what the quiescence pruning was
// doing for every candidate capture.
func IsAttackedBy(b *board.Board, sq board.Sq, by board.Color) bool {
	for _, d := range knightOffsets {
		if p, ok := b.CellPiece(board.Sq{File: sq.File + d[0], Rank: sq.Rank + d[1]}); ok &&
			p.Color == by && p.Type == board.Knight {
			return true
		}
	}
	for _, d := range kingOffsets {
		if p, ok := b.CellPiece(board.Sq{File: sq.File + d[0], Rank: sq.Rank + d[1]}); ok &&
			p.Color == by && p.Type == board.King {
			return true
		}
	}
	for _, d := range rookDirs {
		if hitsSlider(b, sq, d, by, board.Rook, board.Queen) {
			return true
		}
	}
	for _, d := range bishopDirs {
		if hitsSlider(b, sq, d, by, board.Bishop, board.Queen) {
			return true
		}
	}
	byDir := direction[by]
	for _, df := range [2]int{-1, 1} {
		if p, ok := b.CellPiece(board.Sq{File: sq.File + df, Rank: sq.Rank - byDir}); ok &&
			p.Color == by && p.Type == board.Pawn {
			return true
		}
	}
	return false
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

// PinnedSet is a fixed-size set of pinned squares: at most one piece can
// be pinned along each of the 8 rays from the king, so this never needs a
// map (which was the single biggest allocator in the search -- one map
// per node, almost always ending up empty).
type PinnedSet struct {
	squares [8]board.Sq
	count   int
}

func (p *PinnedSet) Has(s board.Sq) bool {
	for i := 0; i < p.count; i++ {
		if p.squares[i] == s {
			return true
		}
	}
	return false
}

func (p *PinnedSet) Len() int { return p.count }

func (p *PinnedSet) add(s board.Sq) {
	p.squares[p.count] = s
	p.count++
}

func PinnedSquares(b *board.Board, color board.Color) PinnedSet {
	king := b.KingSquare(color)
	var pinned PinnedSet

	for _, ry := range pinRays {
		f, r := king.File+ry.d[0], king.Rank+ry.d[1]
		var ownSq board.Sq
		foundOwn := false
		for {
			target := board.Sq{File: f, Rank: r}
			if b.CellOffBoard(target) {
				break
			}
			p, occupied := b.CellPiece(target)
			if occupied {
				if p.Color == color {
					if foundOwn {
						break
					}
					foundOwn = true
					ownSq = target
				} else {
					if foundOwn && (p.Type == ry.slider || p.Type == ry.slider2) {
						pinned.add(ownSq)
					}
					break
				}
			}
			f, r = f+ry.d[0], r+ry.d[1]
		}
	}
	return pinned
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
	rank := 0
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

	empty := func(files ...int) bool {
		for _, f := range files {
			if _, occupied := b.PieceAt(board.Sq{File: f, Rank: rank}); occupied {
				return false
			}
		}
		return true
	}
	safe := func(files ...int) bool {
		for _, f := range files {
			if IsAttackedBy(b, board.Sq{File: f, Rank: rank}, enemy) {
				return false
			}
		}
		return true
	}
	rookAt := func(file int) bool {
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
