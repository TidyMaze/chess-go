// Package game implements turn management, legal move filtering,
// checkmate/stalemate detection, and the threefold-repetition / fifty-move
// draw rules.
package game

import (
	"chess/board"
	"chess/moves"
)

type Move struct {
	From, To board.Sq
}

type Game struct {
	Board           board.Board
	Turn            board.Color
	KingCaptured    bool
	HalfmoveClock   int
	TrackRepetition bool
	positionCounts  map[uint64]int
	// playedBoards holds every position that has occurred in this game,
	// so a search can tell that reaching one again would repeat. Only
	// populated when TrackRepetition is on, which is the real game loops
	// and never the search's own scratch positions.
	playedBoards []board.Board
}

// PlayedBoards returns the positions that have already occurred.
func (g *Game) PlayedBoards() []board.Board { return g.playedBoards }

func New() *Game {
	g := From(board.Initial(), board.White)
	g.TrackRepetition = true
	g.recordPosition()
	return g
}

// From builds a game without repetition tracking. The positionCounts map
// is deliberately left nil: the search allocates one Game per node
// (hundreds of thousands per move), and allocating a map for each one --
// when only the real game-playing loops ever record positions -- was
// pure GC pressure. recordPosition creates it on demand.
func From(b board.Board, turn board.Color) *Game {
	return &Game{Board: b, Turn: turn}
}

// EnableRepetitionTracking turns on threefold-repetition tracking and
// records the current position as the first occurrence. Must be called
// right after construction (not mid-game) or the position count starts
// one occurrence short.
func (g *Game) EnableRepetitionTracking() {
	if g.playedBoards == nil {
		// A game is a few hundred plies; growing this by doubling churns
		// the allocator on every game the generator plays.
		g.playedBoards = make([]board.Board, 0, 256)
	}
	g.TrackRepetition = true
	g.recordPosition()
}

// AllLegalMoves computes pseudo-legal moves once (per piece), then only
// pays for the expensive clone+apply+is-in-check self-check test on king
// moves, pinned pieces, or when already in check -- every other move is
// legal by construction. This is the same optimization validated in the
// Python engine this session (removed ~90% of the expensive checks).
func (g *Game) AllLegalMoves(color board.Color) []Move {
	return g.AppendLegalMoves(nil, color)
}

// AppendLegalMoves is the non-allocating form: the search reuses one
// buffer per node instead of allocating a fresh move slice each time.
func (g *Game) AppendLegalMoves(dst []Move, color board.Color) []Move {
	out, _ := g.AppendLegalMovesInCheck(dst, color)
	return out
}

// AppendLegalMovesInCheck also returns whether the side to move is in
// check, which it has to compute anyway.
//
// The search needs that fact too, for null-move pruning, futility and
// late move reductions, and was calling IsInCheck a second time for it at
// every node. IsInCheck was 12% of search time, so half of that was being
// spent computing something the caller already had.
func (g *Game) AppendLegalMovesInCheck(dst []Move, color board.Color) ([]Move, bool) {
	inCheck := moves.IsInCheck(&g.Board, color)
	return g.appendLegalMoves(dst, color, inCheck), inCheck
}

// AppendLegalMovesGivenCheck appends legal moves when inCheck has already been computed.
func (g *Game) AppendLegalMovesGivenCheck(dst []Move, color board.Color, inCheck bool) []Move {
	return g.appendLegalMoves(dst, color, inCheck)
}

// IsLegalMove reports whether m is legal for the current side to move.
func (g *Game) IsLegalMove(m Move) bool {
	p, ok := g.Board.PieceAt(m.From)
	if !ok || p.Color != g.Turn {
		return false
	}
	if dest, ok := g.Board.PieceAt(m.To); ok {
		if dest.Color == g.Turn || dest.Type == board.King {
			return false
		}
	}
	var targetBuf [28]board.Sq
	targets := moves.AppendLegalTargets(targetBuf[:0], &g.Board, m.From, g.Turn, p.Type)
	found := false
	for _, t := range targets {
		if t == m.To {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	undo := g.Board.MakeMove(m.From, m.To)
	inCheck := moves.IsInCheck(&g.Board, g.Turn)
	g.Board.UnmakeMove(undo)
	return !inCheck
}


func (g *Game) appendLegalMoves(dst []Move, color board.Color, inCheck bool) []Move {
	pinned := moves.PinnedSquares(&g.Board, color)
	var pieceBuf [16]board.PieceAtSquare
	pieces := g.Board.AppendPiecesOf(pieceBuf[:0], color)
	result := dst

	// One target buffer reused across every piece, rather than a fresh
	// slice per piece.
	var targetBuf [28]board.Sq
	epSquare, hasEP := g.Board.EPSquare()
	for _, ps := range pieces {
		needsCheckTest := inCheck || ps.Type == board.King || pinned.Has(ps.Sq)
		for _, target := range moves.AppendLegalTargets(targetBuf[:0], &g.Board, ps.Sq, color, ps.Type) {
			// An en passant capture always needs the full test. It removes
			// a pawn from a square that is neither the origin nor the
			// destination, so it can expose the king along a rank that the
			// pin detection, which only looks at the moving piece, cannot
			// see. This is the "en passant pin" and it is exactly what
			// perft position 3 exists to catch.
			epCapture := hasEP && ps.Type == board.Pawn &&
				target == epSquare && ps.Sq.File != target.File
			if needsCheckTest || epCapture {
				// Make and unmake on the real board rather than cloning
				// it. Cloning copied the whole Board for every candidate
				// move of every piece that could be pinned or in check,
				// which the profile put at ~10% of all CPU at depth 7.
				undo := g.Board.MakeMove(ps.Sq, target)
				illegal := moves.IsInCheck(&g.Board, color)
				g.Board.UnmakeMove(undo)
				if illegal {
					continue
				}
			}
			result = append(result, Move{ps.Sq, target})
		}
	}
	return result
}

func (g *Game) IsCheckmate(color board.Color) bool {
	return moves.IsInCheck(&g.Board, color) && len(g.AllLegalMoves(color)) == 0
}

func (g *Game) IsStalemate(color board.Color) bool {
	return !moves.IsInCheck(&g.Board, color) && len(g.AllLegalMoves(color)) == 0
}

func (g *Game) IsFiftyMoveDraw() bool {
	return g.HalfmoveClock >= 100
}

func (g *Game) IsThreefoldRepetition() bool {
	return g.positionCounts[g.positionKey()] >= 3
}

func (g *Game) IsOver() bool {
	return g.KingCaptured || g.IsCheckmate(g.Turn) || g.IsStalemate(g.Turn) ||
		g.IsFiftyMoveDraw() || g.IsThreefoldRepetition()
}

func (g *Game) ApplyMove(from, to board.Sq) {
	movingPiece, _ := g.Board.PieceAt(from)
	captured, capturedOk := g.Board.PieceAt(to)
	if capturedOk && captured.Type == board.King {
		g.KingCaptured = true
	}

	progress := capturedOk || movingPiece.Type == board.Pawn
	if progress {
		g.HalfmoveClock = 0
	} else {
		g.HalfmoveClock++
	}

	// MakeMove rather than Move: it is the one place that knows a king
	// stepping two files drags a rook with it and gives up the rights.
	// Duplicating that here is how the two paths drift apart.
	g.Board.MakeMove(from, to)

	// Auto-promote to a queen. Underpromotion is legal but is the right
	// choice so rarely that always taking a queen is the standard
	// simplification; without any promotion at all a pawn reaching the
	// last rank would simply have no moves.
	if movingPiece.Type == board.Pawn {
		if (movingPiece.Color == board.White && to.Rank == 7) ||
			(movingPiece.Color == board.Black && to.Rank == 0) {
			g.Board.Place(to, board.Piece{Color: movingPiece.Color, Type: board.Queen})
		}
	}

	g.Turn = g.Turn.Other()

	if g.TrackRepetition {
		g.recordPosition()
	}
}

// CountIfPlayed reports how many times the position resulting from `from
// -> to` has already occurred (0 if never). Only meaningful when
// TrackRepetition is on; lets a move-picker prefer a non-repeating move
// among several that score equally, instead of only discovering the
// repetition after the fact -- a shallow, heuristic-only search has no
// other way to tell a repeating shuffle from real progress, and without
// this it can cycle forever between two equally-scored positions.
func (g *Game) CountIfPlayed(from, to board.Sq) int {
	trial := *g
	trial.Board = g.Board.Clone()
	trial.TrackRepetition = false // don't mutate g's positionCounts map (shared by struct copy)
	trial.Board.Move(from, to)
	trial.Turn = trial.Turn.Other()
	return g.positionCounts[trial.positionKey()]
}

// positionKey encodes piece placement + side to move as a string: cheap
// enough (called once per real move played, not once per search node --
// TrackRepetition is off by default for the search's throwaway positions)
// and simple to get right compared to a numeric zobrist hash.
// positionKey hashes the position into a single integer.
//
// It used to build a sorted string of every piece, which allocated twice
// (the byte slice and the string) on every move of every game, and then
// used that string as a map key, which hashes it a third time. The
// profile put recordPosition at 57% of all bytes allocated in the
// training workload.
//
// The hash is order-independent, so no sort is needed: each piece
// contributes a value mixed by multiplication and combined with XOR,
// which is commutative. A collision would merge two distinct positions
// into one repetition count; at 64 bits over the few hundred positions
// in a game that is not going to happen.
func (g *Game) positionKey() uint64 {
	var h uint64
	var buf [32]board.ColoredPiece
	for _, p := range g.Board.AppendAllPieces(buf[:0]) {
		v := uint64(p.Sq.Rank*8+p.Sq.File)<<8 | uint64(p.Type)<<4 | uint64(p.Color)
		// A cheap integer mix so that neighbouring squares do not produce
		// neighbouring hashes, then XOR so piece order does not matter.
		v *= 0x9E3779B97F4A7C15
		v ^= v >> 29
		h ^= v
	}
	if g.Turn == board.Black {
		h ^= 0xD6E8FEB86659FD93
	}
	return h
}

func (g *Game) recordPosition() {
	if g.positionCounts == nil {
		g.positionCounts = make(map[uint64]int, 128)
	}
	g.positionCounts[g.positionKey()]++
	g.playedBoards = append(g.playedBoards, g.Board)
}

// AppendQuiescenceMoves is AppendLegalMovesInCheck restricted to what the
// quiescence search looks at: every legal move when in check, otherwise
// captures (en passant included) and pawn moves to the last rank. The
// third result says whether any legal move exists at all, so a quiet
// position with no captures is still told apart from a stalemate without
// generating and legality-testing the quiet moves it would never search.
func (g *Game) AppendQuiescenceMoves(dst []Move, color board.Color) ([]Move, bool, bool) {
	inCheck := moves.IsInCheck(&g.Board, color)
	if inCheck {
		result := g.appendLegalMoves(dst, color, true)
		return result, true, len(result) > 0
	}
	pinned := moves.PinnedSquares(&g.Board, color)
	var pieceBuf [16]board.PieceAtSquare
	pieces := g.Board.AppendPiecesOf(pieceBuf[:0], color)
	result := dst
	anyLegal := false
	var targetBuf [28]board.Sq
	epSquare, hasEP := g.Board.EPSquare()
	enemyOcc := g.Board.ColorBitboard(color.Other())
	for _, ps := range pieces {
		needsCheckTest := ps.Type == board.King || pinned.Has(ps.Sq)
		for _, target := range moves.AppendLegalTargets(targetBuf[:0], &g.Board, ps.Sq, color, ps.Type) {
			epCapture := hasEP && ps.Type == board.Pawn &&
				target == epSquare && ps.Sq.File != target.File
			occupied := (enemyOcc & (uint64(1) << (target.Rank*8 + target.File))) != 0
			// Pawns never move backwards, so either end of the board is
			// the last rank for whichever colour is moving.
			promotes := ps.Type == board.Pawn && (target.Rank == 0 || target.Rank == 7)
			wanted := occupied || epCapture || promotes
			// One legal quiet move is all the stalemate question needs;
			// the rest are skipped before their legality test.
			if !wanted && anyLegal {
				continue
			}
			if needsCheckTest || epCapture {
				undo := g.Board.MakeMove(ps.Sq, target)
				illegal := moves.IsInCheck(&g.Board, color)
				g.Board.UnmakeMove(undo)
				if illegal {
					continue
				}
			}
			anyLegal = true
			if wanted {
				result = append(result, Move{ps.Sq, target})
			}
		}
	}
	return result, false, anyLegal
}
