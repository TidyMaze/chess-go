package engine

import (
	"chess/board"
	"chess/game"
)

// advancedPawnPush reports whether a move pushes a pawn to its sixth rank
// or beyond, counted from the mover's side.
//
// Such a push is two moves from queening and changes the evaluation of
// everything around it, so reducing or pruning it costs far more than the
// search saves. Stockfish exempts passed pawn pushes for this reason, and
// an audit here found the engine plays a pawn move half as often as a
// strong oracle does in positions where they disagree.
//
// Rank rather than passedness: a passed pawn test needs the enemy pawn
// bitboards at a point where the move is already made, and the cheap test
// catches the pushes that matter. A pawn on the sixth with enemy pawns in
// front of it is rare enough not to be worth the extra work.
func advancedPawnPush(b *board.Board, m game.Move, mover board.Color) bool {
	p, ok := b.PieceAt(m.From)
	if !ok || p.Type != board.Pawn {
		return false
	}
	rank := m.To.Rank
	if mover == board.Black {
		rank = 7 - rank
	}
	return rank >= 5
}
