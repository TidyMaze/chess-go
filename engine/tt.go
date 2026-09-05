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

// zobristCastle is indexed by the four rights bits directly: 16 entries
// is small enough that one lookup beats combining four separate keys.
var zobristCastle [16]uint64

func init() {
	r := rand.New(rand.NewSource(0x5EED1234)) // fixed seed: reproducible runs
	for p := 0; p < 12; p++ {
		for sq := 0; sq < 64; sq++ {
			zobristPiece[p][sq] = r.Uint64()
		}
	}
	zobristBlackToMove = r.Uint64()
	for i := range zobristCastle {
		zobristCastle[i] = r.Uint64()
	}
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
	// Castling rights are part of the position: two boards with identical
	// pieces but different rights have different legal moves, and without
	// this the table would hand one's score to the other.
	h ^= zobristCastle[g.Board.Castle()&0x0f]
	return h
}

type ttFlag uint8

const (
	ttExact ttFlag = iota
	ttLowerBound
	ttUpperBound
)

// ttEntry is packed to 16 bytes, down from 64.
//
// The table is the hottest memory in the engine: at depth 6-7 the probe
// alone was 38.6% of CPU and the store another 13.1%, over half the
// total, which is not what a direct-mapped array lookup should ever
// cost. The reason was size, not logic. A game.Move is four ints (32
// bytes on its own), so an entry was ~64 bytes and a 2^20 table was
// 64 MB: far past any cache, so essentially every probe was a trip to
// main memory.
//
// So the entry stores what it needs in the smallest form that carries
// it: squares as 0-63 indices rather than pairs of ints, and only the
// upper 32 bits of the key. The low bits already pick the slot, so
// keeping the high half gives a false match roughly once in 4 billion
// probes, which is the standard trade and far cheaper than the cache
// misses it removes.
//
// The score stays float64, which is the one field that cannot shrink.
// Storing it as float32 made the search 18% *bigger* (80,704 nodes at
// depth 6 against 68,277) and 15% slower overall. Principal variation
// search probes with zero-width windows one part in a million wide
// (alpha, alpha+1e-6), and float32 has about seven significant digits,
// so a mate score near 1000 quantises to steps coarser than the window
// itself. Scores came back from the table just different enough to fail
// the window and trigger a re-search.
type ttEntry struct {
	score float64 // pawns; see the note below on why not float32
	key32 uint32  // upper half of the Zobrist key, for verification
	depth int8
	flag  ttFlag
	// maximizingFor matters: a stored score is from one side's point of
	// view, and reusing it for the other side would invert its meaning.
	maximizingFor uint8
	from, to      uint8 // square index, rank*8+file
}

func sqToIndex(s board.Sq) uint8 { return uint8(s.Rank*8 + s.File) }
func indexToSq(i uint8) board.Sq { return board.Sq{File: int(i % 8), Rank: int(i / 8)} }
func keyUpper(key uint64) uint32 { return uint32(key >> 32) }

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
	if e.key32 != keyUpper(key) || int(e.depth) < depth || e.maximizingFor != uint8(maximizingFor) {
		return 0, false
	}
	score := e.score
	switch e.flag {
	case ttExact:
		return score, true
	case ttLowerBound:
		if score >= beta {
			return score, true
		}
	case ttUpperBound:
		if score <= alpha {
			return score, true
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
	if e.key32 == keyUpper(key) && int(e.depth) > depth {
		return
	}
	*e = ttEntry{
		key32: keyUpper(key), score: score, depth: int8(depth), flag: flag,
		maximizingFor: uint8(maximizingFor),
		from:          sqToIndex(best.From), to: sqToIndex(best.To),
	}
}
