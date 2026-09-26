// Package game implements turn management, legal move filtering,
// checkmate/stalemate detection, and the threefold-repetition / fifty-move
// draw rules.
package game

import (
	"math/bits"

	"chess/board"
	"chess/moves"
)

type Move struct {
	From, To board.Sq
	// Promo is the piece a pawn reaching the last rank becomes when that
	// is not a queen. The zero value means a queen: the move generator
	// and the search only ever queen, so their moves leave it unset and
	// compare equal to a parsed "e7e8q". Only moves from outside (a
	// lichess opponent, a GUI, Stockfish, a PGN) carry a knight, bishop
	// or rook here.
	Promo board.PieceType
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
	return g.IsLegalMoveFor(m, g.Turn)
}

// IsLegalMoveFor reports whether m is legal for color, whatever g.Turn
// says. The search plays its moves on the board and leaves g.Turn at the
// root's side, so it has to name the side to move itself.
func (g *Game) IsLegalMoveFor(m Move, color board.Color) bool {
	p, ok := g.Board.PieceAt(m.From)
	if !ok || p.Color != color {
		return false
	}
	if dest, ok := g.Board.PieceAt(m.To); ok {
		if dest.Color == color || dest.Type == board.King {
			return false
		}
	}
	var targetBuf [28]board.Sq
	targets := moves.AppendLegalTargets(targetBuf[:0], &g.Board, m.From, color, p.Type)
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
	inCheck := moves.IsInCheck(&g.Board, color)
	g.Board.UnmakeMove(undo)
	return !inCheck
}

func (g *Game) appendLegalMoves(dst []Move, color board.Color, inCheck bool) []Move {
	result, _ := g.appendMoves(dst, color, inCheck, true)
	return result
}

// Legality tells which moves of one pseudo-legal list may leave the
// mover's king in check. Only those pay for the make, is-in-check, unmake
// test; every other move is known legal or illegal before it is played.
// The zero value tests nothing, for a list that is legal already.
type Legality struct {
	color board.Color
	// suspects holds the origin squares whose moves need the test: the
	// king and the pinned pieces.
	suspects uint64
	// offTarget holds, in check, the squares where a man other than the
	// king does not answer the check. Empty when not in check.
	offTarget uint64
	epSquare  board.Sq
	hasEP     bool
}

// Verdict is what a Legality knows about a move before it is played.
type Verdict uint8

const (
	// Legal by construction: no test needed.
	Legal Verdict = iota
	// Illegal without playing it: it leaves a check standing.
	Illegal
	// Unknown until the move is played and the king asked whether it is
	// in check.
	Unknown
)

// AppendPseudoLegalMoves appends every move of color that is legal except
// possibly for leaving its own king in check, in the order
// AppendLegalMoves returns the legal ones, and the Legality that finishes
// the job one move at a time. A search that cuts off after a few moves
// never pays for the test on the rest.
func (g *Game) AppendPseudoLegalMoves(dst []Move, color board.Color, inCheck bool) ([]Move, Legality) {
	return g.appendMoves(dst, color, inCheck, false)
}

// appendMoves is the one generator behind the legal and the pseudo-legal
// lists, so the two cannot disagree on order. With legalOnly each move is
// tested as it is generated, in the same pass.
func (g *Game) appendMoves(dst []Move, color board.Color, inCheck, legalOnly bool) ([]Move, Legality) {
	king := g.Board.KingSquare(color)
	legality := Legality{
		color:    color,
		suspects: uint64(moves.PinnedSquares(&g.Board, color)) | 1<<(king.Rank*8+king.File),
	}
	if inCheck {
		legality.offTarget = ^evasionTargets(&g.Board, color)
	}
	legality.epSquare, legality.hasEP = g.Board.EPSquare()
	var pieceBuf [16]board.PieceAtSquare
	pieces := g.Board.AppendPiecesOf(pieceBuf[:0], color)
	result := dst
	// One target buffer reused across every piece, rather than a fresh
	// slice per piece.
	var targetBuf [28]board.Sq
	for _, ps := range pieces {
		for _, target := range moves.AppendLegalTargets(targetBuf[:0], &g.Board, ps.Sq, color, ps.Type) {
			m := Move{From: ps.Sq, To: target}
			if legalOnly && !legality.IsLegal(&g.Board, m) {
				continue
			}
			result = append(result, m)
		}
	}
	return result, legality
}

// evasionTargets is where a man other than the king has to land to answer
// a check on color's king: the checker's square, or a square between a
// checking slider and the king. Empty in double check, when only the king
// can move.
func evasionTargets(b *board.Board, color board.Color) uint64 {
	king := b.KingSquare(color)
	k := uint8(king.Rank*8 + king.File)
	enemy := color.Other()
	occupied := b.ColorBitboard(board.White) | b.ColorBitboard(board.Black)
	queens := b.PieceBitboard(enemy, board.Queen)
	rookLines := board.RookAttacks(k, occupied)
	bishopLines := board.BishopAttacks(k, occupied)
	rookCheckers := rookLines & (b.PieceBitboard(enemy, board.Rook) | queens)
	bishopCheckers := bishopLines & (b.PieceBitboard(enemy, board.Bishop) | queens)
	checkers := rookCheckers | bishopCheckers |
		board.PawnAttacksTo[enemy][k]&b.PieceBitboard(enemy, board.Pawn) |
		board.KnightAttacks[k]&b.PieceBitboard(enemy, board.Knight)
	if checkers&(checkers-1) != 0 {
		return 0
	}
	// The king's lines and the checker's, of the kind the checker moves
	// along, cross exactly on the squares strictly between the two.
	switch {
	case rookCheckers != 0:
		return checkers | rookLines&board.RookAttacks(uint8(bits.TrailingZeros64(checkers)), occupied)
	case bishopCheckers != 0:
		return checkers | bishopLines&board.BishopAttacks(uint8(bits.TrailingZeros64(checkers)), occupied)
	}
	return checkers
}

// Screen tells, before m is played, whether it is legal, illegal, or has
// to be played to find out. m must come from the list this Legality came
// with.
func (l Legality) Screen(b *board.Board, m Move) Verdict {
	from := uint64(1) << (m.From.Rank*8 + m.From.File)
	if l.suspects&from != 0 {
		return Unknown
	}
	// An en passant capture always needs the full test. It removes a pawn
	// from a square that is neither the origin nor the destination, so it
	// can expose the king along a rank that the pin detection, which only
	// looks at the moving piece, cannot see. This is the "en passant pin"
	// and it is exactly what perft position 3 exists to catch. For the
	// same reason it can answer a check by a pawn without landing on it.
	if l.hasEP && m.To == l.epSquare && m.From.File != m.To.File &&
		b.PieceBitboard(l.color, board.Pawn)&from != 0 {
		return Unknown
	}
	if l.offTarget&(1<<(m.To.Rank*8+m.To.File)) != 0 {
		return Illegal
	}
	return Legal
}

// NeedsTest reports whether m, from the list this Legality came with, may
// leave the king in check. Must be asked before m is played.
func (l Legality) NeedsTest(b *board.Board, m Move) bool {
	return l.Screen(b, m) != Legal
}

// IsLegal reports whether m, from the list this Legality came with, is
// legal. Make and unmake on the real board rather than cloning it: cloning
// copied the whole Board for every candidate move of every piece that
// could be pinned or in check, which the profile put at ~10% of all CPU at
// depth 7.
func (l Legality) IsLegal(b *board.Board, m Move) bool {
	switch l.Screen(b, m) {
	case Legal:
		return true
	case Illegal:
		return false
	}
	undo := b.MakeMove(m.From, m.To)
	illegal := moves.IsInCheck(b, l.color)
	b.UnmakeMove(undo)
	return !illegal
}

func (g *Game) IsCheckmate(color board.Color) bool {
	return moves.IsInCheck(&g.Board, color) && !g.HasAnyLegalMove(color)
}

func (g *Game) IsStalemate(color board.Color) bool {
	return !moves.IsInCheck(&g.Board, color) && !g.HasAnyLegalMove(color)
}

// HasAnyLegalMove returns true as soon as a single legal move is found for color.
func (g *Game) HasAnyLegalMove(color board.Color) bool {
	inCheck := moves.IsInCheck(&g.Board, color)
	return g.HasAnyLegalMoveInCheck(color, inCheck)
}

// HasAnyLegalMoveInCheck returns true as soon as a single legal move is found,
// accepting precomputed inCheck flag.
func (g *Game) HasAnyLegalMoveInCheck(color board.Color, inCheck bool) bool {
	pinned := moves.PinnedSquares(&g.Board, color)
	var pieceBuf [16]board.PieceAtSquare
	pieces := g.Board.AppendPiecesOf(pieceBuf[:0], color)
	var targetBuf [28]board.Sq
	epSquare, hasEP := g.Board.EPSquare()
	for _, ps := range pieces {
		needsCheckTest := inCheck || ps.Type == board.King || pinned.Has(ps.Sq)
		for _, target := range moves.AppendLegalTargets(targetBuf[:0], &g.Board, ps.Sq, color, ps.Type) {
			epCapture := hasEP && ps.Type == board.Pawn &&
				target == epSquare && ps.Sq.File != target.File
			if needsCheckTest || epCapture {
				undo := g.Board.MakeMove(ps.Sq, target)
				illegal := moves.IsInCheck(&g.Board, color)
				g.Board.UnmakeMove(undo)
				if illegal {
					continue
				}
			}
			return true
		}
	}
	return false
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

// ApplyMove plays from -> to, promoting to a queen.
func (g *Game) ApplyMove(from, to board.Sq) {
	g.Apply(Move{From: from, To: to})
}

// Apply plays m, promoting to m.Promo when it is a knight, bishop or rook
// and to a queen otherwise.
func (g *Game) Apply(m Move) {
	from, to := m.From, m.To
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

	// A queen unless the move names another piece. The engine itself only
	// ever queens, but a move from outside has to land as it was played.
	if movingPiece.Type == board.Pawn {
		if (movingPiece.Color == board.White && to.Rank == 7) ||
			(movingPiece.Color == board.Black && to.Rank == 0) {
			promo := board.Queen
			if m.Promo == board.Knight || m.Promo == board.Bishop || m.Promo == board.Rook {
				promo = m.Promo
			}
			g.Board.Place(to, board.Piece{Color: movingPiece.Color, Type: promo})
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

// positionKey encodes piece placement, side to move, castling rights and
// en passant: cheap
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
		// XOR so piece order does not matter.
		h ^= keyMix(uint64(p.Sq.Rank*8+p.Sq.File)<<8 | uint64(p.Type)<<4 | uint64(p.Color))
	}
	if g.Turn == board.Black {
		h ^= 0xD6E8FEB86659FD93
	}
	// Castling rights and en passant are part of the position (FIDE 9.2).
	// Without them the position after 1.e4 e5, when both sides could
	// still castle, counted with the same pieces after two king walks, and
	// harness and training games ended on a threefold that was not one.
	h ^= keyMix(1<<20 | uint64(g.Board.Castle()))
	if ep, ok := usableEP(&g.Board); ok {
		h ^= keyMix(1<<21 | uint64(ep.File))
	}
	return h
}

// usableEP is the board's en passant square when a pawn beside the pawn
// that just stepped past it can legally take. The board sets the square
// after every double push, but FIDE, lichess and python-chess count it in
// a repeated position only when the capture is legal, so a pinned taker
// does not count.
func usableEP(b *board.Board) (board.Sq, bool) {
	ep, ok := b.EPSquare()
	if !ok {
		return ep, false
	}
	pushedRank, taker := 3, board.Black // a white pawn went to rank 4
	if ep.Rank == 5 {
		pushedRank, taker = 4, board.White // a black pawn went to rank 5
	}
	pawns := b.PieceBitboard(taker, board.Pawn)
	for _, f := range [2]int{ep.File - 1, ep.File + 1} {
		if f < 0 || f >= 8 || pawns&(uint64(1)<<(pushedRank*8+f)) == 0 {
			continue
		}
		undo := b.MakeMove(board.Sq{File: f, Rank: pushedRank}, ep)
		inCheck := moves.IsInCheck(b, taker)
		b.UnmakeMove(undo)
		if !inCheck {
			return ep, true
		}
	}
	return ep, false
}

// keyMix is a cheap integer mix so that neighbouring inputs do not produce
// neighbouring hashes.
func keyMix(v uint64) uint64 {
	v *= 0x9E3779B97F4A7C15
	return v ^ v>>29
}

func (g *Game) recordPosition() {
	if g.positionCounts == nil {
		g.positionCounts = make(map[uint64]int, 128)
	}
	g.positionCounts[g.positionKey()]++
	// The search hashes these boards with the raw en passant square, so
	// drop one no pawn can use: the same pieces a few moves later, without
	// it, are the same position.
	b := g.Board
	if _, ok := usableEP(&b); !ok {
		b.SetEPSquare(board.Sq{}, false)
	}
	g.playedBoards = append(g.playedBoards, b)
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
				result = append(result, Move{From: ps.Sq, To: target})
			}
		}
	}
	return result, false, anyLegal
}
