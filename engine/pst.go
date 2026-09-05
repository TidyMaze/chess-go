package engine

import "chess/board"

// Piece-square tables: a per-piece, per-square positional bonus in the
// same units as material (pawns). Material-only evaluation can't express
// that a knight on the rim is worse than one in the centre, that pawns
// want to advance, or that a king wants to stay tucked away in the
// middlegame -- all of which shape play far more than small differences
// in piece values do.
//
// Tables are written from White's point of view with rank 0 (White's back
// rank) first, and mirrored for Black.
var pstPawn = [64]float64{
	0, 0, 0, 0, 0, 0, 0, 0,
	0.05, 0.10, 0.10, -0.20, -0.20, 0.10, 0.10, 0.05,
	0.05, -0.05, -0.10, 0, 0, -0.10, -0.05, 0.05,
	0, 0, 0, 0.20, 0.20, 0, 0, 0,
	0.05, 0.05, 0.10, 0.25, 0.25, 0.10, 0.05, 0.05,
	0.10, 0.10, 0.20, 0.30, 0.30, 0.20, 0.10, 0.10,
	0.50, 0.50, 0.50, 0.50, 0.50, 0.50, 0.50, 0.50,
	0, 0, 0, 0, 0, 0, 0, 0,
}

var pstKnight = [64]float64{
	-0.50, -0.40, -0.30, -0.30, -0.30, -0.30, -0.40, -0.50,
	-0.40, -0.20, 0, 0.05, 0.05, 0, -0.20, -0.40,
	-0.30, 0.05, 0.10, 0.15, 0.15, 0.10, 0.05, -0.30,
	-0.30, 0, 0.15, 0.20, 0.20, 0.15, 0, -0.30,
	-0.30, 0.05, 0.15, 0.20, 0.20, 0.15, 0.05, -0.30,
	-0.30, 0, 0.10, 0.15, 0.15, 0.10, 0, -0.30,
	-0.40, -0.20, 0, 0, 0, 0, -0.20, -0.40,
	-0.50, -0.40, -0.30, -0.30, -0.30, -0.30, -0.40, -0.50,
}

var pstBishop = [64]float64{
	-0.20, -0.10, -0.10, -0.10, -0.10, -0.10, -0.10, -0.20,
	-0.10, 0.05, 0, 0, 0, 0, 0.05, -0.10,
	-0.10, 0.10, 0.10, 0.10, 0.10, 0.10, 0.10, -0.10,
	-0.10, 0, 0.10, 0.10, 0.10, 0.10, 0, -0.10,
	-0.10, 0.05, 0.05, 0.10, 0.10, 0.05, 0.05, -0.10,
	-0.10, 0, 0.05, 0.10, 0.10, 0.05, 0, -0.10,
	-0.10, 0, 0, 0, 0, 0, 0, -0.10,
	-0.20, -0.10, -0.10, -0.10, -0.10, -0.10, -0.10, -0.20,
}

var pstRook = [64]float64{
	0, 0, 0, 0.05, 0.05, 0, 0, 0,
	-0.05, 0, 0, 0, 0, 0, 0, -0.05,
	-0.05, 0, 0, 0, 0, 0, 0, -0.05,
	-0.05, 0, 0, 0, 0, 0, 0, -0.05,
	-0.05, 0, 0, 0, 0, 0, 0, -0.05,
	-0.05, 0, 0, 0, 0, 0, 0, -0.05,
	0.05, 0.10, 0.10, 0.10, 0.10, 0.10, 0.10, 0.05,
	0, 0, 0, 0, 0, 0, 0, 0,
}

var pstQueen = [64]float64{
	-0.20, -0.10, -0.10, -0.05, -0.05, -0.10, -0.10, -0.20,
	-0.10, 0, 0.05, 0, 0, 0, 0, -0.10,
	-0.10, 0.05, 0.05, 0.05, 0.05, 0.05, 0, -0.10,
	0, 0, 0.05, 0.05, 0.05, 0.05, 0, -0.05,
	-0.05, 0, 0.05, 0.05, 0.05, 0.05, 0, -0.05,
	-0.10, 0, 0.05, 0.05, 0.05, 0.05, 0, -0.10,
	-0.10, 0, 0, 0, 0, 0, 0, -0.10,
	-0.20, -0.10, -0.10, -0.05, -0.05, -0.10, -0.10, -0.20,
}

var pstKing = [64]float64{
	0.20, 0.30, 0.10, 0, 0, 0.10, 0.30, 0.20,
	0.20, 0.20, 0, 0, 0, 0, 0.20, 0.20,
	-0.10, -0.20, -0.20, -0.20, -0.20, -0.20, -0.20, -0.10,
	-0.20, -0.30, -0.30, -0.40, -0.40, -0.30, -0.30, -0.20,
	-0.30, -0.40, -0.40, -0.50, -0.50, -0.40, -0.40, -0.30,
	-0.30, -0.40, -0.40, -0.50, -0.50, -0.40, -0.40, -0.30,
	-0.30, -0.40, -0.40, -0.50, -0.50, -0.40, -0.40, -0.30,
	-0.30, -0.40, -0.40, -0.50, -0.50, -0.40, -0.40, -0.30,
}

var pstTables = [6]*[64]float64{
	board.Pawn: &pstPawn, board.Knight: &pstKnight, board.Bishop: &pstBishop,
	board.Rook: &pstRook, board.Queen: &pstQueen, board.King: &pstKing,
}

// pstValue looks up the positional bonus for a piece, mirroring the rank
// for Black so both sides read the same table from their own viewpoint.
func pstValue(pt board.PieceType, sq board.Sq, color board.Color) float64 {
	rank := sq.Rank
	if color == board.Black {
		rank = 7 - rank
	}
	return pstTables[pt][rank*8+sq.File]
}
