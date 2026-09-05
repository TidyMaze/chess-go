package engine

import (
	"math/rand"

	"chess/board"
	"chess/game"
)

// Zobrist hashing: one random 64-bit key per (piece, square), XORed
// together, plus one for side-to-move. Equal positions hash equal, and
// the hash can be recomputed cheaply. Used to recognise positions the
// search has already evaluated -- the same position is commonly reached
// by different move orders, and without this the search re-explores each
// one from scratch.
var zobristPiece [12][64]uint64
var zobristBlackToMove uint64

func init() {
	r := rand.New(rand.NewSource(0x5EED1234)) // fixed seed: reproducible runs
	for p := 0; p < 12; p++ {
		for sq := 0; sq < 64; sq++ {
			zobristPiece[p][sq] = r.Uint64()
		}
	}
	zobristBlackToMove = r.Uint64()
}

func zobristHash(g *game.Game) uint64 {
	var h uint64
	var buf [16]board.PieceAtSquare
	for _, color := range [2]board.Color{board.White, board.Black} {
		for _, ps := range g.Board.AppendPiecesOf(buf[:0], color) {
			idx := int(color)*6 + int(ps.Type)
			h ^= zobristPiece[idx][ps.Sq.Rank*8+ps.Sq.File]
		}
	}
	if g.Turn == board.Black {
		h ^= zobristBlackToMove
	}
	return h
}

type ttFlag uint8

const (
	ttExact ttFlag = iota
	ttLowerBound
	ttUpperBound
)

type ttEntry struct {
	key   uint64
	score float64
	depth int
	flag  ttFlag
	// maximizingFor matters: a stored score is from one side's point of
	// view, and reusing it for the other side would invert its meaning.
	maximizingFor board.Color
	best          game.Move
}

// TranspositionTable is a fixed-size, direct-mapped cache. No eviction
// policy beyond "newest wins": a deeper entry is worth more, so a
// shallower probe never overwrites a deeper one.
type TranspositionTable struct {
	entries []ttEntry
	mask    uint64
}

func NewTranspositionTable(sizePow2 uint) *TranspositionTable {
	n := uint64(1) << sizePow2
	return &TranspositionTable{entries: make([]ttEntry, n), mask: n - 1}
}

func (t *TranspositionTable) probe(key uint64, depth int, maximizingFor board.Color, alpha, beta float64) (float64, bool) {
	if t == nil {
		return 0, false
	}
	e := &t.entries[key&t.mask]
	if e.key != key || e.depth < depth || e.maximizingFor != maximizingFor {
		return 0, false
	}
	switch e.flag {
	case ttExact:
		return e.score, true
	case ttLowerBound:
		if e.score >= beta {
			return e.score, true
		}
	case ttUpperBound:
		if e.score <= alpha {
			return e.score, true
		}
	}
	return 0, false
}

func (t *TranspositionTable) store(key uint64, score float64, depth int, flag ttFlag, maximizingFor board.Color) {
	t.storeWithMove(key, score, depth, flag, maximizingFor, game.Move{})
}

// storeWithMove also remembers the best move found, which the next
// iterative-deepening pass tries first -- the main reason iterative
// deepening ends up cheaper than searching the target depth directly.
func (t *TranspositionTable) storeWithMove(key uint64, score float64, depth int, flag ttFlag, maximizingFor board.Color, best game.Move) {
	if t == nil {
		return
	}
	e := &t.entries[key&t.mask]
	if e.key == key && e.depth > depth {
		return
	}
	*e = ttEntry{key: key, score: score, depth: depth, flag: flag, maximizingFor: maximizingFor, best: best}
}
