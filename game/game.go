// Package game implements turn management, legal move filtering,
// checkmate/stalemate detection, and the threefold-repetition / fifty-move
// draw rules.
package game

import (
	"chess/board"
	"chess/moves"
	"sort"
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
	positionCounts  map[string]int
}

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
	inCheck := moves.IsInCheck(&g.Board, color)
	pinned := moves.PinnedSquares(&g.Board, color)
	var pieceBuf [16]board.PieceAtSquare
	pieces := g.Board.AppendPiecesOf(pieceBuf[:0], color)
	result := dst

	// One target buffer reused across every piece, rather than a fresh
	// slice per piece.
	var targetBuf [28]board.Sq
	for _, ps := range pieces {
		needsCheckTest := inCheck || ps.Type == board.King || pinned.Has(ps.Sq)
		for _, target := range moves.AppendLegalTargets(targetBuf[:0], &g.Board, ps.Sq, color, ps.Type) {
			if needsCheckTest {
				trial := g.Board.Clone()
				trial.Move(ps.Sq, target)
				if moves.IsInCheck(&trial, color) {
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

	g.Board.Move(from, to)
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
func (g *Game) positionKey() string {
	white := g.Board.PiecesOf(board.White)
	black := g.Board.PiecesOf(board.Black)
	sortPieces(white)
	sortPieces(black)
	key := make([]byte, 0, 128)
	for _, p := range white {
		key = append(key, byte('A'+p.Type), byte('a'+p.Sq.File), byte('0'+p.Sq.Rank))
	}
	key = append(key, '|')
	for _, p := range black {
		key = append(key, byte('A'+p.Type), byte('a'+p.Sq.File), byte('0'+p.Sq.Rank))
	}
	key = append(key, byte('0'+int(g.Turn)))
	return string(key)
}

func sortPieces(pieces []board.PieceAtSquare) {
	sort.Slice(pieces, func(i, j int) bool {
		if pieces[i].Sq.File != pieces[j].Sq.File {
			return pieces[i].Sq.File < pieces[j].Sq.File
		}
		return pieces[i].Sq.Rank < pieces[j].Sq.Rank
	})
}

func (g *Game) recordPosition() {
	if g.positionCounts == nil {
		g.positionCounts = map[string]int{}
	}
	g.positionCounts[g.positionKey()]++
}
