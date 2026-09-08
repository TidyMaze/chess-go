package engine

import (
	"chess/board"
	"chess/game"
	"chess/moves"
)

// see is the static exchange evaluation of m, in pawns: the material the
// side to move ends up with once every capture on the destination square
// has been played out, each side using its least valuable attacker first
// and either side free to stop. A quiet move is evaluated the same way
// with nothing captured first, which says whether the moved piece can
// simply be taken.
//
// It works on a copy of the board, removing each capturer from its
// square as it goes, so sliders behind a capturer come into play (the
// x-ray). A king may capture only if nothing would recapture it, since
// that capture would be illegal.
func see(b *board.Board, m game.Move) int {
	mover, ok := b.PieceAt(m.From)
	if !ok {
		return 0
	}
	sim := b.Clone()
	// At most 31 captures can follow: there are 32 pieces.
	var gain [33]int
	if victim, has := sim.PieceAt(m.To); has {
		gain[0] = mvvLvaPiece[victim.Type]
	} else if mover.Type == board.Pawn && m.From.File != m.To.File {
		// A diagonal pawn move to an empty square is en passant: the
		// captured pawn stands beside the mover, not on the destination.
		gain[0] = mvvLvaPiece[board.Pawn]
		sim.Remove(board.Sq{File: m.To.File, Rank: m.From.Rank})
	}
	sim.Remove(m.From)
	sim.Place(m.To, mover)
	onSquare := mvvLvaPiece[mover.Type]
	side := mover.Color.Other()
	d := 0
	for {
		from, typ, found := leastValuableAttacker(&sim, m.To, side)
		if !found {
			break
		}
		if typ == board.King && moves.IsAttackedBy(&sim, m.To, side.Other()) {
			break
		}
		d++
		gain[d] = onSquare - gain[d-1]
		onSquare = mvvLvaPiece[typ]
		sim.Remove(from)
		sim.Place(m.To, board.Piece{Color: side, Type: typ})
		side = side.Other()
	}
	// Each side may stop the sequence, so a capture is worth the better
	// of stopping and continuing: gain[d-1] = -max(-gain[d-1], gain[d]).
	for ; d > 0; d-- {
		if gain[d] > -gain[d-1] {
			gain[d-1] = -gain[d]
		}
	}
	return gain[0]
}

var seeOrder = [...]board.PieceType{board.Pawn, board.Knight, board.Bishop, board.Rook, board.Queen, board.King}

// leastValuableAttacker finds the cheapest piece of colour by that attacks
// sq, scanning pawns, then knights, bishops, rooks, queens, and the king.
func leastValuableAttacker(b *board.Board, sq board.Sq, by board.Color) (board.Sq, board.PieceType, bool) {
	for _, typ := range seeOrder {
		if from, ok := moves.AttackerOfType(b, sq, by, typ); ok {
			return from, typ, true
		}
	}
	return board.Sq{}, 0, false
}

// seePrunes is the main-search rule for a losing capture: at depth 6 or
// less, on a zero-window node, a capture whose exchange evaluation is
// worse than a pawn per ply of depth is not searched. Never in check and
// never a checking capture.
func seePrunes(depth, exchange int, inCheck, givesCheck, zeroWin bool) bool {
	return depth <= 6 && zeroWin && !inCheck && !givesCheck && exchange < -depth
}
