package engine

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"chess/board"
	"chess/game"
	"chess/moves"
)

// Endgame tablebases, generated here rather than downloaded.
//
// Syzygy is the standard answer and it is not a realistic one for this
// project: the format is a compressed indexed structure that would be a
// large piece of work to read correctly, and the seven-piece set is
// terabytes. Retrograde analysis over the small piece sets is a different
// proposition. Every position with three or four pieces can be
// enumerated, and working backwards from the mates gives exact
// distance-to-mate for all of them in seconds.
//
// What this buys: exactness where the evaluation is guessing. A hand
// written evaluation converts king and rook against king by nudging the
// lone king toward the edge, which measured 88% reliable; a tablebase
// makes it 100% and makes the drawn cases provably drawn, so the engine
// stops trying to win them and stops avoiding them.
//
// The method is standard (Ströhlein 1970): mark every immediate mate as
// distance 0, then repeatedly find positions where the side to move is
// lost in n and mark their predecessors, alternating sides.

// Tablebase holds exact results for one material configuration.
type Tablebase struct {
	// Pieces is the configuration, White's men then Black's, kings first.
	Pieces []board.ColoredPiece
	// dtm maps a position index to signed distance to mate in plies:
	// positive means the side to move is winning in that many plies,
	// negative means the side to move is losing, zero means drawn.
	dtm map[uint32]int16
}

// TablebaseSet is several configurations, looked up by the material on
// the board.
type TablebaseSet struct {
	byKey map[string]*Tablebase
}

func (t *TablebaseSet) Len() int {
	if t == nil {
		return 0
	}
	n := 0
	for _, tb := range t.byKey {
		n += len(tb.dtm)
	}
	return n
}

// materialKey names the configuration on the board, for example "KQvK".
// Kings are always present so they anchor the string, which makes the key
// readable in a file listing and in an error message.
// pieceLetters is indexed by PieceType rather than looked up in a map.
// The probe runs at every evaluated node with four pieces or fewer, so a
// map lookup and a string concatenation here cost 156 ns per evaluation,
// measured, which is three times what the whole endgame evaluation costs.
var pieceLetters = [...]byte{
	board.King: 'K', board.Queen: 'Q', board.Rook: 'R',
	board.Bishop: 'B', board.Knight: 'N', board.Pawn: 'P',
}

// materialKey writes the configuration into dst and returns it, so the
// caller can keep the buffer on its stack and the key never reaches the
// heap.
func materialKey(dst []byte, b *board.Board) ([]byte, int) {
	var buf [32]board.ColoredPiece
	pieces := b.AppendAllPieces(buf[:0])
	if len(pieces) > 5 {
		return nil, len(pieces)
	}
	var w, bl [5]byte
	nw, nb := 0, 0
	for _, p := range pieces {
		c := pieceLetters[p.Type]
		if p.Color == board.White {
			if nw == len(w) {
				return nil, len(pieces)
			}
			w[nw] = c
			nw++
		} else {
			if nb == len(bl) {
				return nil, len(pieces)
			}
			bl[nb] = c
			nb++
		}
	}
	sortBytes(w[:nw])
	sortBytes(bl[:nb])
	dst = append(dst, w[:nw]...)
	dst = append(dst, 'v')
	dst = append(dst, bl[:nb]...)
	return dst, len(pieces)
}

// pieceOrder ranks the letters so the same material always produces the
// same key. An array, again, because this is on the probe's hot path.
var pieceOrder = func() (t [256]int8) {
	for i := range t {
		t[i] = 9
	}
	t['K'], t['Q'], t['R'], t['B'], t['N'], t['P'] = 0, 1, 2, 3, 4, 5
	return
}()

// sortBytes puts the king first and the rest in a fixed order.
func sortBytes(s []byte) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && pieceOrder[s[j]] < pieceOrder[s[j-1]]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Probe returns the exact score in pawns and whether the position is in
// the tablebase.
//
// A won position is scored near the mate range and closer mates score
// higher, so the search prefers a faster win; a draw is exactly zero,
// which is what stops the engine from thrashing in a dead position.
func (t *TablebaseSet) Probe(b *board.Board, stm board.Color) (float64, bool) {
	if t == nil || len(t.byKey) == 0 {
		return 0, false
	}
	var keyBuf [12]byte
	key, n := materialKey(keyBuf[:0], b)
	if len(key) == 0 || n == 0 {
		return 0, false
	}
	// Looking up a map with a []byte key does not allocate; converting to
	// string first would.
	tb := t.byKey[string(key)]
	if tb == nil {
		return 0, false
	}
	idx, ok := encodePosition(b, stm, tb.Pieces)
	if !ok {
		return 0, false
	}
	d, ok := tb.dtm[idx]
	if !ok {
		return 0, false
	}
	score := dtmToScore(d)
	// dtm is from the side to move; the evaluation is from White.
	if stm == board.Black {
		score = -score
	}
	return score, true
}

// probeDTM returns the distance to mate from the side to move, and
// whether this set covers the material at all.
//
// The two answers are different and the difference matters during
// generation: a position whose material is covered but absent from the
// map is a known draw, while material that is not covered is unknown and
// must not be treated as a draw.
func (t *TablebaseSet) probeDTM(b *board.Board, stm board.Color) (dtm int16, covered bool) {
	if t == nil || len(t.byKey) == 0 {
		return 0, false
	}
	var keyBuf [12]byte
	key, n := materialKey(keyBuf[:0], b)
	if len(key) == 0 || n == 0 {
		return 0, false
	}
	tb := t.byKey[string(key)]
	if tb == nil {
		return 0, false
	}
	idx, ok := encodePosition(b, stm, tb.Pieces)
	if !ok {
		return 0, false
	}
	return tb.dtm[idx], true
}

// dtmToScore maps distance to mate onto the evaluation's scale. Mates are
// worth more than any material, and a mate in three beats a mate in nine.
func dtmToScore(d int16) float64 {
	switch {
	case d == 0:
		return 0
	case d > 0:
		return 100 - float64(d)/10
	default:
		return -100 - float64(d)/10
	}
}

// encodePosition packs a position into an index. The piece list fixes the
// order, so the same position always encodes the same way.
func encodePosition(b *board.Board, stm board.Color, order []board.ColoredPiece) (uint32, bool) {
	var buf [32]board.ColoredPiece
	pieces := b.AppendAllPieces(buf[:0])
	if len(pieces) != len(order) {
		return 0, false
	}
	var used [8]bool
	if len(pieces) > len(used) {
		return 0, false
	}
	idx := uint32(0)
	for _, want := range order {
		found := -1
		for i, p := range pieces {
			if !used[i] && p.Color == want.Color && p.Type == want.Type {
				found = i
				break
			}
		}
		if found < 0 {
			return 0, false
		}
		used[found] = true
		sq := pieces[found].Sq
		idx = idx*64 + uint32(sq.Rank*8+sq.File)
	}
	idx = idx*2 + uint32(stm)
	return idx, true
}

// Save writes a set to disk. Generation is seconds, but seconds on every
// process start is still worse than a file.
func (t *TablebaseSet) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return t.writeSet(f)
}

// writeSet writes the set in its file format. Separate from Save so a test
// can hand it a writer that fails, which a real file only does when the
// disk is full.
func (t *TablebaseSet) writeSet(f io.Writer) error {
	if err := binary.Write(f, binary.LittleEndian, uint32(len(t.byKey))); err != nil {
		return err
	}
	for key, tb := range t.byKey {
		if err := writeString(f, key); err != nil {
			return err
		}
		if err := binary.Write(f, binary.LittleEndian, uint32(len(tb.Pieces))); err != nil {
			return err
		}
		for _, p := range tb.Pieces {
			if _, err := f.Write([]byte{byte(p.Color), byte(p.Type)}); err != nil {
				return err
			}
		}
		if err := binary.Write(f, binary.LittleEndian, uint32(len(tb.dtm))); err != nil {
			return err
		}
		var rec [6]byte
		for idx, d := range tb.dtm {
			binary.LittleEndian.PutUint32(rec[0:4], idx)
			binary.LittleEndian.PutUint16(rec[4:6], uint16(d))
			if _, err := f.Write(rec[:]); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeString(f io.Writer, s string) error {
	if err := binary.Write(f, binary.LittleEndian, uint32(len(s))); err != nil {
		return err
	}
	_, err := io.WriteString(f, s)
	return err
}

func LoadTablebases(path string) (*TablebaseSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	set := &TablebaseSet{byKey: map[string]*Tablebase{}}
	pos := 0
	need := func(n int) bool { return pos+n <= len(data) }
	if !need(4) {
		return nil, fmt.Errorf("tablebase: file is empty")
	}
	nSets := int(binary.LittleEndian.Uint32(data[pos:]))
	pos += 4
	for i := 0; i < nSets; i++ {
		if !need(4) {
			return nil, fmt.Errorf("tablebase: truncated at set %d", i)
		}
		kl := int(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4
		if !need(kl) {
			return nil, fmt.Errorf("tablebase: truncated key at set %d", i)
		}
		key := string(data[pos : pos+kl])
		pos += kl
		if !need(4) {
			return nil, fmt.Errorf("tablebase: truncated piece count for %s", key)
		}
		np := int(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4
		tb := &Tablebase{dtm: map[uint32]int16{}}
		for j := 0; j < np; j++ {
			if !need(2) {
				return nil, fmt.Errorf("tablebase: truncated pieces for %s", key)
			}
			tb.Pieces = append(tb.Pieces, board.ColoredPiece{
				Color: board.Color(data[pos]), Type: board.PieceType(data[pos+1]),
			})
			pos += 2
		}
		if !need(4) {
			return nil, fmt.Errorf("tablebase: truncated entry count for %s", key)
		}
		n := int(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4
		for j := 0; j < n; j++ {
			if !need(6) {
				return nil, fmt.Errorf("tablebase: truncated entries for %s", key)
			}
			idx := binary.LittleEndian.Uint32(data[pos:])
			d := int16(binary.LittleEndian.Uint16(data[pos+4:]))
			tb.dtm[idx] = d
			pos += 6
		}
		set.byKey[key] = tb
	}
	return set, nil
}

// ---------------------------------------------------------------------
// generation

// GenerateTablebase builds the exact table for one material
// configuration by retrograde analysis.
//
// It works forwards rather than backwards, which is slower in theory and
// far simpler in practice: enumerate every position, then repeatedly
// propagate "lost in n" and "won in n+1" until nothing changes. For three
// and four pieces the state space is small enough (at most 64^4 x 2, and
// almost all of it illegal) that the simplicity is worth more than the
// asymptotics.
func GenerateTablebase(pieces []board.ColoredPiece, prior *TablebaseSet) *Tablebase {
	tb := &Tablebase{Pieces: append([]board.ColoredPiece(nil), pieces...), dtm: map[uint32]int16{}}

	type state struct {
		g   *game.Game
		idx uint32
	}
	var all []state

	// Enumerate every placement of the listed pieces on distinct squares,
	// for both sides to move, keeping only legal positions.
	n := len(pieces)
	squares := make([]int, n)
	var walk func(int)
	walk = func(k int) {
		if k == n {
			for _, stm := range []board.Color{board.White, board.Black} {
				g, ok := buildPosition(pieces, squares, stm)
				if !ok {
					continue
				}
				idx, ok := encodePosition(&g.Board, stm, pieces)
				if !ok {
					continue
				}
				all = append(all, state{g, idx})
			}
			return
		}
		for sq := 0; sq < 64; sq++ {
			dup := false
			for i := 0; i < k; i++ {
				if squares[i] == sq {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			// Pawns cannot stand on the first or last rank.
			if pieces[k].Type == board.Pawn && (sq < 8 || sq >= 56) {
				continue
			}
			squares[k] = sq
			walk(k + 1)
		}
	}
	walk(0)

	// Everything starts as drawn, then mates are marked and the result
	// propagates outwards.
	known := make(map[uint32]int16, len(all))
	byIdx := make(map[uint32]*game.Game, len(all))
	for _, st := range all {
		byIdx[st.idx] = st.g
		if len(st.g.AllLegalMoves(st.g.Turn)) == 0 {
			if moves.IsInCheck(&st.g.Board, st.g.Turn) {
				known[st.idx] = -1 // mated: losing in zero moves
			} else {
				known[st.idx] = 0 // stalemate is a draw
			}
		}
	}

	// Iterate to a fixed point. Each pass can only add results, so this
	// terminates, and the number of passes is the longest mate.
	for pass := 0; ; pass++ {
		changed := false
		for _, st := range all {
			if _, done := known[st.idx]; done {
				continue
			}
			g := st.g
			legal := g.AllLegalMoves(g.Turn)
			if len(legal) == 0 {
				continue
			}
			bestWin, bestLoss := int16(0), int16(0)
			allKnownLosing := true
			for _, m := range legal {
				child, sameMaterial := applyForTablebase(g, m)
				var d int16
				var ok bool
				if sameMaterial {
					var cidx uint32
					cidx, ok = encodePosition(&child.Board, child.Turn, pieces)
					if ok {
						d, ok = known[cidx]
					}
				} else {
					// A capture or a promotion leaves this configuration.
					// Its value lives in a smaller or different table, and
					// resolving it there is what makes king and pawn
					// against king a win at all: every win in that ending
					// runs through a promotion, so without this the whole
					// table came out drawn.
					d, ok = prior.probeDTM(&child.Board, child.Turn)
				}
				if !ok {
					allKnownLosing = false
					continue
				}
				switch {
				case d < 0:
					// The opponent is losing there, so this move wins.
					w := int16(-d + 1)
					if bestWin == 0 || w < bestWin {
						bestWin = w
					}
					allKnownLosing = false
				case d == 0:
					allKnownLosing = false
				default:
					// Every child known so far leaves us losing.
					if l := int16(-(d + 1)); bestLoss == 0 || l > bestLoss {
						bestLoss = l
					}
				}
			}
			if bestWin > 0 {
				known[st.idx] = bestWin
				changed = true
			} else if allKnownLosing && bestLoss < 0 {
				known[st.idx] = bestLoss
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// Draws are stored too, not left absent.
	//
	// Leaving them out makes the tablebase asymmetric in the way that
	// matters most: the engine sees a won king and pawn ending as a win
	// and a *drawn* one as "I am a pawn up", so it trades into dead draws
	// believing it is winning. Measured, the decisive-only table was worth
	// -26 +/- 31 Elo while improving rook endgame conversion, which is
	// what a table that answers half the question looks like.
	//
	// A position that survived the fixed point without being decided is
	// drawn, so everything enumerated gets an entry.
	for _, st := range all {
		tb.dtm[st.idx] = known[st.idx]
	}
	return tb
}

// buildPosition places the pieces and rejects positions that cannot occur:
// kings adjacent, or the side not to move already in check.
func buildPosition(pieces []board.ColoredPiece, squares []int, stm board.Color) (*game.Game, bool) {
	b := board.NewEmpty()
	for i, p := range pieces {
		b.Place(board.Sq{File: squares[i] % 8, Rank: squares[i] / 8},
			board.Piece{Color: p.Color, Type: p.Type})
	}
	g := game.From(b, stm)
	// The side that just moved cannot still be in check.
	if moves.IsInCheck(&g.Board, stm.Other()) {
		return nil, false
	}
	return g, true
}

// applyForTablebase makes a move on a copy. A move that changes the
// material (a capture or a promotion) leaves this table's configuration
// and is reported as unavailable, since the answer lives in a different
// table.
func applyForTablebase(g *game.Game, m game.Move) (child *game.Game, sameMaterial bool) {
	// A value copy of the game is enough: Board is an array and the
	// repetition tracking is nil here, so nothing is shared.
	c := *g
	child = &c
	before := countPieces(&child.Board)
	wasPromotion := promoted(g, m)
	child.ApplyMove(m.From, m.To)
	return child, countPieces(&child.Board) == before && !wasPromotion
}

func countPieces(b *board.Board) int {
	var buf [32]board.ColoredPiece
	return len(b.AppendAllPieces(buf[:0]))
}

func promoted(g *game.Game, m game.Move) bool {
	p, ok := g.Board.PieceAt(m.From)
	if !ok || p.Type != board.Pawn {
		return false
	}
	return m.To.Rank == 0 || m.To.Rank == 7
}

// StandardTablebases is the set worth generating: the three-piece endings
// plus the four-piece ones the engine actually reaches. King and rook
// against king is the important one, because converting it is exactly
// where a hand-written evaluation is guessing.
func StandardTablebases() [][]board.ColoredPiece {
	wk := board.ColoredPiece{Color: board.White, Type: board.King}
	bk := board.ColoredPiece{Color: board.Black, Type: board.King}
	w := func(t board.PieceType) board.ColoredPiece {
		return board.ColoredPiece{Color: board.White, Type: t}
	}
	b := func(t board.PieceType) board.ColoredPiece {
		return board.ColoredPiece{Color: board.Black, Type: t}
	}
	// Dependency order: a pawn ending promotes into a queen ending, and
	// any ending can be captured down to bare kings, so the smaller
	// tables must exist before the larger ones are generated.
	return [][]board.ColoredPiece{
		{wk, bk},
		{wk, w(board.Queen), bk},
		{wk, w(board.Rook), bk},
		{wk, bk, b(board.Queen)},
		{wk, bk, b(board.Rook)},
		{wk, w(board.Pawn), bk},
		{wk, bk, b(board.Pawn)},
		{wk, w(board.Queen), bk, b(board.Rook)},
		{wk, w(board.Rook), bk, b(board.Rook)},
		{wk, w(board.Queen), bk, b(board.Queen)},
		{wk, w(board.Rook), bk, b(board.Pawn)},
		{wk, w(board.Pawn), bk, b(board.Pawn)},
	}
}

// BuildTablebases generates every configuration in the list.
// The list must be in dependency order, fewest pieces first: a
// configuration is generated against the ones already built, because a
// capture or a promotion lands in one of them.
func BuildTablebases(sets [][]board.ColoredPiece, progress func(key string, entries int)) *TablebaseSet {
	out := &TablebaseSet{byKey: map[string]*Tablebase{}}
	for _, pieces := range sets {
		tb := GenerateTablebase(pieces, out)
		b := board.NewEmpty()
		for i, p := range pieces {
			b.Place(board.Sq{File: i % 8, Rank: i / 8}, board.Piece{Color: p.Color, Type: p.Type})
		}
		var keyBuf [12]byte
		key, _ := materialKey(keyBuf[:0], &b)
		out.byKey[string(key)] = tb
		if progress != nil {
			progress(string(key), len(tb.dtm))
		}
	}
	return out
}

// TablebaseMaxPieces is the largest configuration generated. The probe is
// skipped above it with a single comparison, so a middlegame position
// never pays for the tablebase existing.
const TablebaseMaxPieces = 4
