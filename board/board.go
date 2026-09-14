// Package board implements the chess board as a padded flat array: the
// real 8x8 board sits inside a 12x12 grid (2 squares of padding on every
// side), so move generation never needs a separate bounds check -- any
// offset up to 2 squares (a knight jump) from an on-board square lands
// safely inside the array, in padding if it went off the real board.
package board

type Color int

const (
	White Color = iota
	Black
)

func (c Color) Other() Color {
	if c == White {
		return Black
	}
	return White
}

type PieceType int

const (
	Pawn PieceType = iota
	Knight
	Bishop
	Rook
	Queen
	King
)

type Piece struct {
	Color Color
	Type  PieceType
}

type Sq struct {
	File, Rank int
}

const pad = 2
const width = 8 + 2*pad

// Cells are encoded in a single byte: 0 = off-board, 1 = empty, and
// 2+ = an occupied square (colour*6 + type + 2). A struct-per-cell
// representation made Board 3.4KB, and the search copies a Board per
// node -- one byte per cell makes it 144 bytes instead.
type cellCode uint8

const (
	codeOffBoard cellCode = 0
	codeEmpty    cellCode = 1
	codePieceMin cellCode = 2
)

func encodePiece(p Piece) cellCode {
	return codePieceMin + cellCode(int(p.Color)*6+int(p.Type))
}

// decoded is the piece for every cell code, so a probe is one table read.
// The arithmetic version (a division and a modulo per cell) was 9% of the
// search's CPU on its own: every move generator and check test decodes
// dozens of cells per node.
var decoded = func() (t [codePieceMin + 12]Piece) {
	for c := codePieceMin; c < codePieceMin+12; c++ {
		v := int(c - codePieceMin)
		t[c] = Piece{Color: Color(v / 6), Type: PieceType(v % 6)}
	}
	return t
}()

func decodePiece(c cellCode) Piece {
	return decoded[c]
}

// Board is a value type: copying it (Clone) is a plain array copy, no
// allocation or per-piece work, unlike a map-keyed representation.
// Castling rights, one bit each. Rights are board state, not game state:
// they are lost by moves (and by a rook being captured), so they have to
// be saved and restored by make/unmake alongside the pieces.
const (
	WhiteKingSide uint8 = 1 << iota
	WhiteQueenSide
	BlackKingSide
	BlackQueenSide
	AllCastling = WhiteKingSide | WhiteQueenSide | BlackKingSide | BlackQueenSide
)

type Board struct {
	cells [width * width]cellCode
	kings [2]Sq
	// Square indices (rank*8+file), not Sq values. A Sq is two ints, so
	// this array alone was 512 bytes and dominated both the Board and the
	// Undo record: the legal-move generator clones a board per candidate
	// move and the search makes and unmakes one per node, so those 512
	// bytes were being copied constantly. As indices it is 32 bytes.
	occupied      [32]uint8
	occupiedCount int
	castle        uint8
	// epSquare is the square a pawn may be captured on by en passant,
	// as a 0-63 index, or noEP. It is board state for the same reason
	// castling rights are: it is created and destroyed by moves, so
	// make/unmake has to save and restore it.
	epSquare uint8

	pieces  [2][6]uint64
	colorBB [2]uint64
}

// PieceBitboard returns the 64-bit bitboard for color c and piece type pt.
func (b *Board) PieceBitboard(c Color, pt PieceType) uint64 {
	return b.pieces[c][pt]
}

// ColorBitboard returns the 64-bit bitboard of all pieces of color c.
func (b *Board) ColorBitboard(c Color) uint64 {
	return b.colorBB[c]
}

// noEP marks "no en passant capture is available".
const noEP uint8 = 64

// EPSquare returns the en passant target square and whether there is one.
func (b *Board) EPSquare() (Sq, bool) {
	if b.epSquare == noEP {
		return Sq{}, false
	}
	return squareFromIndex(b.epSquare), true
}

// SetEPSquare sets the en passant target (used by the FEN parser).
func (b *Board) SetEPSquare(s Sq, ok bool) {
	if !ok {
		b.epSquare = noEP
		return
	}
	b.epSquare = squareIndex(s)
}

// Castle reports the current castling rights.
func (b *Board) Castle() uint8 { return b.castle }

// SetCastle replaces the castling rights (used by the FEN parser).
func (b *Board) SetCastle(c uint8) { b.castle = c }

// castlingLost maps a square to the rights that end when a piece moves
// from it or is captured on it. A king leaving e1 ends both white
// rights; a rook leaving (or dying on) a1 ends only the queenside one.
func castlingLost(s Sq) uint8 {
	switch {
	case s.Rank == 0 && s.File == 4:
		return WhiteKingSide | WhiteQueenSide
	case s.Rank == 0 && s.File == 0:
		return WhiteQueenSide
	case s.Rank == 0 && s.File == 7:
		return WhiteKingSide
	case s.Rank == 7 && s.File == 4:
		return BlackKingSide | BlackQueenSide
	case s.Rank == 7 && s.File == 0:
		return BlackQueenSide
	case s.Rank == 7 && s.File == 7:
		return BlackKingSide
	}
	return 0
}

func squareIndex(s Sq) uint8 { return uint8(s.Rank*8 + s.File) }

func squareFromIndex(i uint8) Sq { return Sq{File: int(i & 7), Rank: int(i >> 3)} }

func index(s Sq) int {
	return (s.Rank+pad)*width + (s.File + pad)
}

func Initial() Board {
	var b Board
	for rank := 0; rank < 8; rank++ {
		for file := 0; file < 8; file++ {
			b.cells[index(Sq{file, rank})] = codeEmpty
		}
	}
	backRank := [8]PieceType{Rook, Knight, Bishop, Queen, King, Bishop, Knight, Rook}
	for file, pt := range backRank {
		b.setPiece(Sq{file, 0}, Piece{White, pt})
		b.setPiece(Sq{file, 1}, Piece{White, Pawn})
		b.setPiece(Sq{file, 6}, Piece{Black, Pawn})
		b.setPiece(Sq{file, 7}, Piece{Black, pt})
	}
	b.castle = AllCastling
	b.epSquare = noEP
	return b
}

// NewEmpty returns an empty 8x8 board (no pieces) for building custom test
// positions, mirroring the Python project's `Board({...})` constructor.
func NewEmpty() Board {
	var b Board
	b.epSquare = noEP
	for rank := 0; rank < 8; rank++ {
		for file := 0; file < 8; file++ {
			b.cells[index(Sq{file, rank})] = codeEmpty
		}
	}
	return b
}

func (b *Board) Place(s Sq, p Piece) {
	b.setPiece(s, p)
}

func (b *Board) setPiece(s Sq, p Piece) {
	wasEmpty := b.cells[index(s)] < codePieceMin
	sqBit := uint64(1) << squareIndex(s)
	if !wasEmpty {
		old := decodePiece(b.cells[index(s)])
		b.pieces[old.Color][old.Type] &^= sqBit
		b.colorBB[old.Color] &^= sqBit
	}
	b.cells[index(s)] = encodePiece(p)
	b.pieces[p.Color][p.Type] |= sqBit
	b.colorBB[p.Color] |= sqBit
	if p.Type == King {
		b.kings[p.Color] = s
	}
	if wasEmpty {
		b.occupied[b.occupiedCount] = squareIndex(s)
		b.occupiedCount++
	}
}

// PieceAt is the safe, bounds-checked accessor for external callers.
func (b *Board) PieceAt(s Sq) (Piece, bool) {
	if s.File < 0 || s.File >= 8 || s.Rank < 0 || s.Rank >= 8 {
		return Piece{}, false
	}
	c := b.cells[index(s)]
	if c < codePieceMin {
		return Piece{}, false
	}
	return decodePiece(c), true
}

// CellOffBoard/CellPiece: fast unchecked accessors for move generation.
// Safe for any square reachable by one offset (up to 2 squares) from an
// on-board square; not safe for arbitrary external input.
func (b *Board) CellOffBoard(s Sq) bool {
	return b.cells[index(s)] == codeOffBoard
}

func (b *Board) CellPiece(s Sq) (Piece, bool) {
	c := b.cells[index(s)]
	if c < codePieceMin {
		return Piece{}, false
	}
	return decodePiece(c), true
}

// HitsSlider scans along ray (df, dr) from square from. It returns true if the first
// piece encountered is an enemy piece matching t1 or t2, false otherwise.
var (
	knightDeltas = [8]int{
		2*width + 1, 2*width - 1, -2*width + 1, -2*width - 1,
		1*width + 2, 1*width - 2, -1*width + 2, -1*width - 2,
	}
	kingDeltas = [8]int{
		1*width + 1, 1 * width, 1*width - 1,
		-1, 1,
		-1*width + 1, -1 * width, -1*width - 1,
	}
	rookDeltas   = [4]int{width, -width, 1, -1}
	bishopDeltas = [4]int{width + 1, width - 1, -width + 1, -width - 1}
)

func (b *Board) hitsSliderDelta(idx, delta int, enemy Color, t1, t2 PieceType) bool {
	for {
		idx += delta
		code := b.cells[idx]
		if code == codeOffBoard {
			return false
		}
		if code == codeEmpty {
			continue
		}
		p := decodePiece(code)
		return p.Color == enemy && (p.Type == t1 || p.Type == t2)
	}
}

func (b *Board) HitsSlider(from Sq, df, dr int, enemy Color, t1, t2 PieceType) bool {
	return b.hitsSliderDelta(index(from), dr*width+df, enemy, t1, t2)
}

// FindPinnedPiece scans ray (df, dr) from king. If exactly one piece of ownColor is on the ray
// and the first piece behind it is an enemy slider of type s1 or s2, it returns the pinned square and true.
func (b *Board) FindPinnedPiece(king Sq, df, dr int, ownColor Color, s1, s2 PieceType) (Sq, bool) {
	idx := index(king)
	step := dr*width + df
	foundOwn := false
	var ownIdx int
	for {
		idx += step
		c := b.cells[idx]
		if c == codeOffBoard {
			return Sq{}, false
		}
		if c == codeEmpty {
			continue
		}
		p := decoded[c]
		if p.Color == ownColor {
			if foundOwn {
				return Sq{}, false
			}
			foundOwn = true
			ownIdx = idx
		} else {
			if foundOwn && (p.Type == s1 || p.Type == s2) {
				return Sq{File: (ownIdx % width) - pad, Rank: (ownIdx / width) - pad}, true
			}
			return Sq{}, false
		}
	}
}

// IsAttackedBy reports whether sq is attacked by side by.
func (b *Board) IsAttackedBy(sq Sq, by Color) bool {
	sqIdx := squareIndex(sq)

	// Pawn attacks
	if PawnAttacksTo[by][sqIdx]&b.pieces[by][Pawn] != 0 {
		return true
	}

	// Knight attacks
	if KnightAttacks[sqIdx]&b.pieces[by][Knight] != 0 {
		return true
	}

	// King attacks
	if KingAttacks[sqIdx]&(1<<squareIndex(b.kings[by])) != 0 {
		return true
	}

	baseIdx := index(sq)

	// Sliders: Rook / Queen
	for _, d := range rookDeltas {
		if b.hitsSliderDelta(baseIdx, d, by, Rook, Queen) {
			return true
		}
	}

	// Sliders: Bishop / Queen
	for _, d := range bishopDeltas {
		if b.hitsSliderDelta(baseIdx, d, by, Bishop, Queen) {
			return true
		}
	}

	return false
}

// IsInCheck reports whether the king of color is in check.
func (b *Board) IsInCheck(color Color) bool {
	return b.IsAttackedBy(b.kings[color], color.Other())
}

// AppendSlideMoves appends to dst all pseudo-legal target squares for a slider at sq moving along dirs.
func (b *Board) AppendSlideMoves(dst []Sq, sq Sq, color Color, dirs [][2]int) []Sq {
	moves := dst
	baseIdx := index(sq)
	for _, d := range dirs {
		delta := d[1]*width + d[0]
		currIdx := baseIdx
		f, r := sq.File, sq.Rank
		for {
			currIdx += delta
			code := b.cells[currIdx]
			if code == codeOffBoard {
				break
			}
			f += d[0]
			r += d[1]
			target := Sq{File: f, Rank: r}
			if code == codeEmpty {
				moves = append(moves, target)
				continue
			}
			if decodePiece(code).Color != color {
				moves = append(moves, target)
			}
			break
		}
	}
	return moves
}

// AppendStepMoves appends to dst all pseudo-legal target squares for a stepper at sq using offsets.
func (b *Board) AppendStepMoves(dst []Sq, sq Sq, color Color, offsets [][2]int) []Sq {
	moves := dst
	baseIdx := index(sq)
	for _, d := range offsets {
		idx := baseIdx + d[1]*width + d[0]
		code := b.cells[idx]
		if code == codeOffBoard {
			continue
		}
		if code == codeEmpty || decodePiece(code).Color != color {
			moves = append(moves, Sq{File: sq.File + d[0], Rank: sq.Rank + d[1]})
		}
	}
	return moves
}

// PieceCount is the number of men on the board. Kept as a counter rather
// than recounted, because the endgame tablebase probe needs to reject the
// vast majority of positions before doing anything expensive, and it is
// consulted at every evaluated node.
func (b *Board) PieceCount() int { return b.occupiedCount }

func (b *Board) KingSquare(c Color) Sq {
	return b.kings[c]
}

func (b *Board) Move(from, to Sq) {
	fromIdx, toIdx := index(from), index(to)
	moved := b.cells[fromIdx]
	captured := b.cells[toIdx] >= codePieceMin
	if captured {
		cap := decodePiece(b.cells[toIdx])
		capBit := uint64(1) << squareIndex(to)
		b.pieces[cap.Color][cap.Type] &^= capBit
		b.colorBB[cap.Color] &^= capBit
	}
	b.cells[fromIdx] = codeEmpty
	b.cells[toIdx] = moved
	if moved >= codePieceMin {
		p := decodePiece(moved)
		if p.Type == King {
			b.kings[p.Color] = to
		}
		fromBit := uint64(1) << squareIndex(from)
		toBit := uint64(1) << squareIndex(to)
		b.pieces[p.Color][p.Type] = (b.pieces[p.Color][p.Type] &^ fromBit) | toBit
		b.colorBB[p.Color] = (b.colorBB[p.Color] &^ fromBit) | toBit
	}

	// Keep the occupied list in step. Order matters: drop the captured
	// piece's entry for `to` first, then move the mover's entry from
	// `from` to `to` -- otherwise the two entries collide.
	if captured {
		toIdx := squareIndex(to)
		for i := 0; i < b.occupiedCount; i++ {
			if b.occupied[i] == toIdx {
				b.occupied[i] = b.occupied[b.occupiedCount-1]
				b.occupiedCount--
				break
			}
		}
	}
	fromIdxSq := squareIndex(from)
	for i := 0; i < b.occupiedCount; i++ {
		if b.occupied[i] == fromIdxSq {
			b.occupied[i] = squareIndex(to)
			break
		}
	}
}

// Clone is a plain struct copy (Board holds only arrays), not a
// map/dict copy: this is the single biggest win of the flat-array
// design over a map[Sq]Piece representation for the search hot path.
func (b Board) Clone() Board {
	return b
}

type PieceAtSquare struct {
	Sq   Sq
	Type PieceType
}

// PiecesOf returns (square, type) pairs for every piece of color c --
// the form move generation needs (it must know where each piece is).
func (b *Board) PiecesOf(c Color) []PieceAtSquare {
	return b.AppendPiecesOf(nil, c)
}

// AppendPiecesOf is the non-allocating form: the caller supplies the
// buffer (a stack array is enough -- there are at most 16 pieces per
// side), which matters because this runs once per search node.
//
// It walks only the occupied squares recorded in occupied[], not all 64:
// a full-board scan was ~18% of search CPU, and by the endgame most of
// those squares are empty.
func (b *Board) AppendPiecesOf(dst []PieceAtSquare, c Color) []PieceAtSquare {
	result := dst
	for i := 0; i < b.occupiedCount; i++ {
		s := squareFromIndex(b.occupied[i])
		cl := b.cells[index(s)]
		if cl >= codePieceMin {
			if p := decodePiece(cl); p.Color == c {
				result = append(result, PieceAtSquare{s, p.Type})
			}
		}
	}
	return result
}

// Undo captures everything Move changes, so a move can be taken back
// without copying the board. The search visits hundreds of thousands of
// nodes per move and copied a whole Board into a new Game at each one;
// make/unmake removes that copy entirely.
type Undo struct {
	from, to    Sq
	movedCode   cellCode
	capturedRaw cellCode
	kings       [2]Sq
	occupied    [32]uint8
	occCount    int
	castle      uint8
	// rookFrom/rookTo record the rook's half of a castling move so unmake
	// can put it back. Zero value means this was not a castling move.
	rookFrom, rookTo Sq
	wasCastling      bool
	// epSquare before the move, and the pawn an en passant capture
	// removed, which sits on neither the from nor the to square and so is
	// not restored by the ordinary cell writes.
	epSquare     uint8
	epCaptured   Sq
	wasEPCapture bool
	pieces       [2][6]uint64
	colorBB      [2]uint64
}

func (u Undo) MovedCode() uint8   { return uint8(u.movedCode) }
func (u Undo) CapturedRaw() uint8 { return uint8(u.capturedRaw) }
func (u Undo) Castle() uint8      { return u.castle }
func (u Undo) OldEPSquare() (Sq, bool) {
	if u.epSquare == noEP {
		return Sq{}, false
	}
	return squareFromIndex(u.epSquare), true
}
func (u Undo) RookFrom() Sq       { return u.rookFrom }
func (u Undo) RookTo() Sq         { return u.rookTo }
func (u Undo) WasCastling() bool  { return u.wasCastling }
func (u Undo) EPCaptured() Sq     { return u.epCaptured }
func (u Undo) WasEPCapture() bool { return u.wasEPCapture }

// MakeMove applies a move and returns what is needed to undo it.
func (b *Board) MakeMove(from, to Sq) Undo {
	u := Undo{
		from:        from,
		to:          to,
		movedCode:   b.cells[index(from)],
		capturedRaw: b.cells[index(to)],
		kings:       b.kings,
		occupied:    b.occupied,
		occCount:    b.occupiedCount,
		castle:      b.castle,
		epSquare:    b.epSquare,
		pieces:      b.pieces,
		colorBB:     b.colorBB,
	}
	// A king stepping two files is a castling move, and the rook has to
	// travel with it. Detected here rather than encoded in Move so that
	// every path that moves a piece (search, game, UI) gets it.
	if p, ok := b.PieceAt(from); ok && p.Type == King && abs(to.File-from.File) == 2 {
		u.wasCastling = true
		if to.File > from.File {
			u.rookFrom = Sq{File: 7, Rank: from.Rank}
			u.rookTo = Sq{File: 5, Rank: from.Rank}
		} else {
			u.rookFrom = Sq{File: 0, Rank: from.Rank}
			u.rookTo = Sq{File: 3, Rank: from.Rank}
		}
	}
	// En passant: a pawn moving diagonally onto the empty target square
	// captures the pawn that passed it, which stands beside the mover
	// rather than on the destination.
	if p, ok := b.PieceAt(from); ok && p.Type == Pawn && b.epSquare != noEP &&
		squareIndex(to) == b.epSquare && from.File != to.File {
		if _, occupied := b.PieceAt(to); !occupied {
			victim := Sq{File: to.File, Rank: from.Rank}
			u.wasEPCapture = true
			u.epCaptured = victim
			b.Remove(victim)
		}
	}

	b.Move(from, to)
	if u.wasCastling {
		b.Move(u.rookFrom, u.rookTo)
	}
	b.castle &^= castlingLost(from) | castlingLost(to)

	// A pawn that has just stepped two squares can be captured en passant
	// on the square it skipped, but only on the very next move.
	b.epSquare = noEP
	if p, ok := b.PieceAt(to); ok && p.Type == Pawn && abs(to.Rank-from.Rank) == 2 {
		b.epSquare = squareIndex(Sq{File: from.File, Rank: (from.Rank + to.Rank) / 2})
	}
	return u
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// UnmakeMove restores the position saved in u.
func (b *Board) UnmakeMove(u Undo) {
	if u.wasCastling {
		// Put the rook back first: the cell writes below restore only the
		// king's two squares.
		b.cells[index(u.rookFrom)] = b.cells[index(u.rookTo)]
		b.cells[index(u.rookTo)] = codeEmpty
	}
	b.cells[index(u.from)] = u.movedCode
	b.cells[index(u.to)] = u.capturedRaw
	b.kings = u.kings
	b.occupied = u.occupied
	b.occupiedCount = u.occCount
	b.castle = u.castle
	b.epSquare = u.epSquare
	b.pieces = u.pieces
	b.colorBB = u.colorBB
	if u.wasEPCapture {

		// The captured pawn stood on neither square the writes above
		// touched, so its cell has to be restored explicitly. The
		// occupied list is restored wholesale from the undo record and
		// already contains it.
		mover := decodePiece(u.movedCode)
		b.cells[index(u.epCaptured)] = encodePiece(Piece{Color: mover.Color.Other(), Type: Pawn})
	}
}

// SetPiece replaces the piece on a square (used for promotion).
func (b *Board) SetPiece(s Sq, p Piece) { b.setPiece(s, p) }

// ColoredPiece is a piece with its colour, for callers that want every
// piece on the board in one pass rather than one pass per side.
type ColoredPiece struct {
	Sq    Sq
	Type  PieceType
	Color Color
}

// AppendAllPieces walks the occupied list once and returns both sides.
//
// AppendPiecesOf filters by colour, so anything wanting both sides pays
// for two walks. The evaluation wanted seven (phase, material and tables
// per side, pawn files per side, structure per side), which made a single
// evaluation walk the piece list seven times over.
func (b *Board) AppendAllPieces(dst []ColoredPiece) []ColoredPiece {
	result := dst
	for i := 0; i < b.occupiedCount; i++ {
		s := squareFromIndex(b.occupied[i])
		cl := b.cells[index(s)]
		if cl >= codePieceMin {
			p := decodePiece(cl)
			result = append(result, ColoredPiece{s, p.Type, p.Color})
		}
	}
	return result
}

// Remove clears a square and drops it from the occupied list.
//
// Needed for en passant, where the captured pawn stands on neither the
// square the capturing pawn left nor the one it lands on, so none of the
// ordinary move bookkeeping touches it.
func (b *Board) Remove(s Sq) {
	cl := b.cells[index(s)]
	if cl < codePieceMin {
		return
	}
	p := decodePiece(cl)
	sqBit := uint64(1) << squareIndex(s)
	b.pieces[p.Color][p.Type] &^= sqBit
	b.colorBB[p.Color] &^= sqBit
	b.cells[index(s)] = codeEmpty
	idx := squareIndex(s)
	for i := 0; i < b.occupiedCount; i++ {
		if b.occupied[i] == idx {
			b.occupied[i] = b.occupied[b.occupiedCount-1]
			b.occupiedCount--
			return
		}
	}
}
