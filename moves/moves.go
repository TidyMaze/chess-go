// Package moves implements pseudo-legal move generation, check detection,
// and pin detection for the chess engine.
package moves

import "chess/board"

var startRank = map[board.Color]int{board.White: 1, board.Black: 6}
var direction = map[board.Color]int{board.White: 1, board.Black: -1}

var knightOffsets = [8][2]int{{1, 2}, {1, -2}, {-1, 2}, {-1, -2}, {2, 1}, {2, -1}, {-2, 1}, {-2, -1}}
var kingOffsets = [8][2]int{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}
var bishopDirs = [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
var rookDirs = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

func stepMoves(b *board.Board, sq board.Sq, color board.Color, offsets [][2]int) []board.Sq {
	moves := make([]board.Sq, 0, len(offsets))
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

func slideMoves(b *board.Board, sq board.Sq, color board.Color, dirs [][2]int) []board.Sq {
	moves := make([]board.Sq, 0, 8)
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

func pawnMoves(b *board.Board, sq board.Sq, color board.Color) []board.Sq {
	dir := direction[color]
	moves := make([]board.Sq, 0, 4)

	oneAhead := board.Sq{File: sq.File, Rank: sq.Rank + dir}
	if _, occupied := b.CellPiece(oneAhead); !occupied {
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
		}
	}
	return moves
}

func dirsAsSlice(dirs [4][2]int) [][2]int {
	out := make([][2]int, 4)
	copy(out, dirs[:])
	return out
}

func offsetsAsSlice8(offsets [8][2]int) [][2]int {
	out := make([][2]int, 8)
	copy(out, offsets[:])
	return out
}

// LegalTargets returns pseudo-legal target squares (not yet filtered for
// leaving your own king in check -- that's game.AllLegalMoves's job).
func LegalTargets(b *board.Board, sq board.Sq, color board.Color, pt board.PieceType) []board.Sq {
	switch pt {
	case board.Pawn:
		return pawnMoves(b, sq, color)
	case board.Knight:
		return stepMoves(b, sq, color, offsetsAsSlice8(knightOffsets))
	case board.King:
		return stepMoves(b, sq, color, offsetsAsSlice8(kingOffsets))
	case board.Bishop:
		return slideMoves(b, sq, color, dirsAsSlice(bishopDirs))
	case board.Rook:
		return slideMoves(b, sq, color, dirsAsSlice(rookDirs))
	case board.Queen:
		return slideMoves(b, sq, color, append(dirsAsSlice(bishopDirs), dirsAsSlice(rookDirs)...))
	}
	panic("unknown piece type")
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

// PinnedSquares returns the set of color's own squares that are pinned to
// its king: moving that piece could expose check, so its legality needs
// the expensive per-move self-check test. Computed once per position
// instead of once per candidate move -- this is what makes legal move
// generation fast.
func PinnedSquares(b *board.Board, color board.Color) map[board.Sq]bool {
	king := b.KingSquare(color)
	pinned := map[board.Sq]bool{}

	type ray struct {
		d     [2]int
		types []board.PieceType
	}
	rays := make([]ray, 0, 8)
	for _, d := range rookDirs {
		rays = append(rays, ray{d, []board.PieceType{board.Rook, board.Queen}})
	}
	for _, d := range bishopDirs {
		rays = append(rays, ray{d, []board.PieceType{board.Bishop, board.Queen}})
	}

	for _, ry := range rays {
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
					if foundOwn {
						for _, t := range ry.types {
							if p.Type == t {
								pinned[ownSq] = true
							}
						}
					}
					break
				}
			}
			f, r = f+ry.d[0], r+ry.d[1]
		}
	}
	return pinned
}
