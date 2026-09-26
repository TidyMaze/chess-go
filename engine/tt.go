package engine

import (
	"math/bits"
	"math/rand"
	"sync"

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

// zobristEP is indexed by file: the rank is implied by whose turn it is.
var zobristEP [8]uint64

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
	for i := range zobristEP {
		zobristEP[i] = r.Uint64()
	}
}

func zobristHash(g *game.Game) uint64 {
	return zobristBoard(&g.Board, g.Turn)
}

// zobristBoard hashes a board plus whose turn it is. Split out from
// zobristHash so positions from the game's history, which are boards
// without a Game around them, can be hashed the same way.
func zobristBoard(b *board.Board, turn board.Color) uint64 {
	var h uint64
	for c := board.White; c <= board.Black; c++ {
		for pt := board.Pawn; pt <= board.King; pt++ {
			idx := int(c)*6 + int(pt)
			bb := b.PieceBitboard(c, pt)
			for bb != 0 {
				sq := bits.TrailingZeros64(bb)
				bb &= bb - 1
				h ^= zobristPiece[idx][sq]
			}
		}
	}
	if turn == board.Black {
		h ^= zobristBlackToMove
	}
	// Castling rights are part of the position: two boards with identical
	// pieces but different rights have different legal moves, and without
	// this the table would hand one's score to the other.
	h ^= zobristCastle[b.Castle()&0x0f]
	// En passant availability changes the legal moves, so two otherwise
	// identical positions are different positions.
	if sq, ok := b.EPSquare(); ok {
		h ^= zobristEP[sq.File]
	}
	return h
}

// zobristUpdate updates key incrementally across a move.
func zobristUpdate(key uint64, b *board.Board, m game.Move, undo board.Undo, promoted bool) uint64 {
	key ^= zobristBlackToMove

	oldCastle := undo.Castle() & 0x0f
	newCastle := b.Castle() & 0x0f
	if oldCastle != newCastle {
		key ^= zobristCastle[oldCastle] ^ zobristCastle[newCastle]
	}

	if oldEP, ok := undo.OldEPSquare(); ok {
		key ^= zobristEP[oldEP.File]
	}
	if newEP, ok := b.EPSquare(); ok {
		key ^= zobristEP[newEP.File]
	}

	movedCode := undo.MovedCode()
	movedIdx := int(movedCode - 2)
	fromSq := m.From.Rank*8 + m.From.File
	toSq := m.To.Rank*8 + m.To.File
	key ^= zobristPiece[movedIdx][fromSq]

	if promoted {
		queenIdx := movedIdx + 4
		key ^= zobristPiece[queenIdx][toSq]
	} else {
		key ^= zobristPiece[movedIdx][toSq]
	}

	if capCode := undo.CapturedRaw(); capCode >= 2 {
		capIdx := int(capCode - 2)
		key ^= zobristPiece[capIdx][toSq]
	}

	if undo.WasEPCapture() {
		epSq := undo.EPCaptured()
		pawnIdx := int(board.Pawn)
		if movedIdx < 6 {
			pawnIdx += 6
		}
		key ^= zobristPiece[pawnIdx][epSq.Rank*8+epSq.File]
	}

	if undo.WasCastling() {
		rFrom := undo.RookFrom()
		rTo := undo.RookTo()
		rookIdx := int(board.Rook)
		if movedIdx >= 6 {
			rookIdx += 6
		}
		key ^= zobristPiece[rookIdx][rFrom.Rank*8+rFrom.File] ^ zobristPiece[rookIdx][rTo.Rank*8+rTo.File]
	}

	return key
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
	// gen is the search this entry was written in. An entry from an older
	// search is stale and may be replaced whatever its depth, which is what
	// stops a depth-preferred table silting up and never accepting anything.
	gen uint8
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
	// shared is set once the table is handed to more than one search
	// thread. Only then do probe and store take a lock, so a single-threaded
	// search pays nothing for the possibility.
	shared bool
	locks  [ttStripes]sync.Mutex
	// generation counts searches, so entries from earlier ones can be
	// recycled whatever their depth.
	generation uint8
}

// ttStripes is how many locks a shared table spreads its slots over. Ten
// threads over a thousand stripes rarely meet.
const ttStripes = 1024

// share marks the table as used by several threads at once.
func (t *TranspositionTable) share() { t.shared = true }

func NewTranspositionTable(sizePow2 uint) *TranspositionTable {
	n := uint64(1) << sizePow2
	return &TranspositionTable{entries: make([]ttEntry, n), mask: n - 1}
}

// scoreToTT turns a mate score, which the search counts from the root, into
// a distance from the node being stored, and scoreFromTT turns it back at
// the ply it is probed from. The same position is reached at many plies,
// and a raw mate score stored at one of them claims the wrong distance at
// every other: two plies deeper it promises a mate two plies too soon. That
// was enough, with the old depth-left scores, to make every mate look alike
// and let a won ending run into the fifty-move rule. Scores that are not
// mates are positions, not distances, and pass through untouched.
func scoreToTT(score float64, ply int) float64 {
	switch {
	case score >= mateBound:
		return score + float64(ply)
	case score <= -mateBound:
		return score - float64(ply)
	}
	return score
}

func scoreFromTT(score float64, ply int) float64 {
	switch {
	case score >= mateBound:
		return score - float64(ply)
	case score <= -mateBound:
		return score + float64(ply)
	}
	return score
}

// probe converts the stored score to this ply before comparing it with the
// window, since alpha and beta are root-relative too.
func (t *TranspositionTable) probe(key uint64, depth, ply int, maximizingFor board.Color, alpha, beta float64) (float64, bool) {
	if t == nil {
		return 0, false
	}
	idx := key & t.mask
	var e *ttEntry
	var stackEntry ttEntry
	if t.shared {
		l := &t.locks[idx&(ttStripes-1)]
		l.Lock()
		stackEntry = t.entries[idx]
		l.Unlock()
		e = &stackEntry
	} else {
		e = &t.entries[idx]
	}
	if e.key32 != keyUpper(key) || int(e.depth) < depth || e.maximizingFor != uint8(maximizingFor) {
		return 0, false
	}
	score := scoreFromTT(e.score, ply)
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

// probeWithMove probes for both a cutoff score and the stored best move in a single lookup and lock.
func (t *TranspositionTable) probeWithMove(key uint64, depth, ply int, maximizingFor board.Color, alpha, beta float64) (score float64, cutoff bool, m game.Move, okMove bool) {
	if t == nil {
		return 0, false, game.Move{}, false
	}
	idx := key & t.mask
	var e *ttEntry
	var stackEntry ttEntry
	if t.shared {
		l := &t.locks[idx&(ttStripes-1)]
		l.Lock()
		stackEntry = t.entries[idx]
		l.Unlock()
		e = &stackEntry
	} else {
		e = &t.entries[idx]
	}
	if e.key32 != keyUpper(key) {
		return 0, false, game.Move{}, false
	}
	m = game.Move{From: indexToSq(e.from), To: indexToSq(e.to)}
	okMove = true
	if int(e.depth) < depth || e.maximizingFor != uint8(maximizingFor) {
		return 0, false, m, true
	}
	score = scoreFromTT(e.score, ply)
	switch e.flag {
	case ttExact:
		return score, true, m, true
	case ttLowerBound:
		if score >= beta {
			return score, true, m, true
		}
	case ttUpperBound:
		if score <= alpha {
			return score, true, m, true
		}
	}
	return 0, false, m, true
}

func (t *TranspositionTable) store(key uint64, score float64, depth, ply int, flag ttFlag, maximizingFor board.Color) {
	t.storeWithMove(key, score, depth, ply, flag, maximizingFor, game.Move{})
}

// storeWithMove also remembers the best move found, which the next
// iterative-deepening pass tries first -- the main reason iterative
// deepening ends up cheaper than searching the target depth directly.
func (t *TranspositionTable) storeWithMove(key uint64, score float64, depth, ply int, flag ttFlag, maximizingFor board.Color, best game.Move) {
	if t == nil {
		return
	}
	idx := key & t.mask
	entry := ttEntry{
		key32: keyUpper(key), score: scoreToTT(score, ply), depth: int8(depth), flag: flag,
		maximizingFor: uint8(maximizingFor),
		from:          sqToIndex(best.From), to: sqToIndex(best.To),
	}
	if !t.shared {
		t.put(idx, depth, &entry)
		return
	}
	l := &t.locks[idx&(ttStripes-1)]
	l.Lock()
	t.put(idx, depth, &entry)
	l.Unlock()
}

// bestMove is the move stored for key, if the slot still holds that key.
// It takes the same lock as probe and store on a shared table; the move
// ordering used to read the slot directly, which raced with a helper
// thread's store.
func (t *TranspositionTable) bestMove(key uint64) (game.Move, bool) {
	if t == nil {
		return game.Move{}, false
	}
	idx := key & t.mask
	var e *ttEntry
	var stackEntry ttEntry
	if t.shared {
		l := &t.locks[idx&(ttStripes-1)]
		l.Lock()
		stackEntry = t.entries[idx]
		l.Unlock()
		e = &stackEntry
	} else {
		e = &t.entries[idx]
	}
	if e.key32 != keyUpper(key) {
		return game.Move{}, false
	}
	return game.Move{From: indexToSq(e.from), To: indexToSq(e.to)}, true
}

// put writes an entry unless the slot already holds a deeper one for the
// same key.
// entryFor returns the stored depth, flag and score for a key, for the
// singular test, which needs to know how much the table actually knows
// about a move rather than just what the move is.
func (t *TranspositionTable) entryFor(key uint64) (score float64, depth int, flag ttFlag, ok bool) {
	if t == nil {
		return 0, 0, 0, false
	}
	idx := key & t.mask
	var e *ttEntry
	var stackEntry ttEntry
	if t.shared {
		l := &t.locks[idx&(ttStripes-1)]
		l.Lock()
		stackEntry = t.entries[idx]
		l.Unlock()
		e = &stackEntry
	} else {
		e = &t.entries[idx]
	}
	if e.key32 != keyUpper(key) {
		return 0, 0, 0, false
	}
	return e.score, int(e.depth), e.flag, true
}

// put is depth-preferred within a search and always-replace across
// searches. Quiescence stores at depth 0 and is about half the nodes, so
// plain always-replace let a leaf evict a depth-12 entry it collided with,
// and the subtree behind that entry had to be searched again.
func (t *TranspositionTable) put(idx uint64, depth int, entry *ttEntry) {
	e := &t.entries[idx]
	entry.gen = t.generation
	if e.key32 == entry.key32 && int(e.depth) > depth {
		return
	}
	if e.gen == t.generation && int(e.depth) > depth {
		return
	}
	*e = *entry
}

// NewSearch ages the table: every entry written before this point becomes
// replaceable regardless of its depth.
func (t *TranspositionTable) NewSearch() {
	if t != nil {
		t.generation++
	}
}
