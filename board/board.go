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

func decodePiece(c cellCode) Piece {
	v := int(c - codePieceMin)
	return Piece{Color: Color(v / 6), Type: PieceType(v % 6)}
}

// Board is a value type: copying it (Clone) is a plain array copy, no
// allocation or per-piece work, unlike a map-keyed representation.
type Board struct {
	cells [width * width]cellCode
	kings [2]Sq
}

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
	return b
}

// NewEmpty returns an empty 8x8 board (no pieces) for building custom test
// positions, mirroring the Python project's `Board({...})` constructor.
func NewEmpty() Board {
	var b Board
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
	b.cells[index(s)] = encodePiece(p)
	if p.Type == King {
		b.kings[p.Color] = s
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

func (b *Board) KingSquare(c Color) Sq {
	return b.kings[c]
}

func (b *Board) Move(from, to Sq) {
	fromIdx, toIdx := index(from), index(to)
	moved := b.cells[fromIdx]
	b.cells[fromIdx] = codeEmpty
	b.cells[toIdx] = moved
	if moved >= codePieceMin {
		if p := decodePiece(moved); p.Type == King {
			b.kings[p.Color] = to
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
func (b *Board) AppendPiecesOf(dst []PieceAtSquare, c Color) []PieceAtSquare {
	result := dst
	for rank := 0; rank < 8; rank++ {
		for file := 0; file < 8; file++ {
			cl := b.cells[index(Sq{file, rank})]
			if cl >= codePieceMin {
				if p := decodePiece(cl); p.Color == c {
					result = append(result, PieceAtSquare{Sq{file, rank}, p.Type})
				}
			}
		}
	}
	return result
}
