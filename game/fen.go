package game

import (
	"fmt"
	"strconv"
	"strings"

	"chess/board"
)

var fenLetters = [6]byte{
	board.Pawn: 'p', board.Knight: 'n', board.Bishop: 'b',
	board.Rook: 'r', board.Queen: 'q', board.King: 'k',
}

// FEN renders the position in Forsyth-Edwards Notation, so an external
// UCI engine can be given the same position this engine is looking at.
//
// Castling rights and en passant are both real, so the FEN describes
// exactly the position this engine is looking at and an external engine
// asked about it sees the same one.
func (g *Game) FEN() string {
	var sb strings.Builder
	for rank := 7; rank >= 0; rank-- {
		empty := 0
		for file := 0; file < 8; file++ {
			p, ok := g.Board.PieceAt(board.Sq{File: file, Rank: rank})
			if !ok {
				empty++
				continue
			}
			if empty > 0 {
				fmt.Fprintf(&sb, "%d", empty)
				empty = 0
			}
			ch := fenLetters[p.Type]
			if p.Color == board.White {
				ch = ch - 'a' + 'A'
			}
			sb.WriteByte(ch)
		}
		if empty > 0 {
			fmt.Fprintf(&sb, "%d", empty)
		}
		if rank > 0 {
			sb.WriteByte('/')
		}
	}

	side := "w"
	if g.Turn == board.Black {
		side = "b"
	}
	rights := ""
	for _, r := range []struct {
		bit  uint8
		char string
	}{
		{board.WhiteKingSide, "K"}, {board.WhiteQueenSide, "Q"},
		{board.BlackKingSide, "k"}, {board.BlackQueenSide, "q"},
	} {
		if g.Board.Castle()&r.bit != 0 {
			rights += r.char
		}
	}
	if rights == "" {
		rights = "-"
	}
	ep := "-"
	if sq, ok := g.Board.EPSquare(); ok {
		ep = string(rune('a'+sq.File)) + string(rune('1'+sq.Rank))
	}
	fullMove := g.HalfmoveClock/2 + 1
	return fmt.Sprintf("%s %s %s %s %d %d", sb.String(), side, rights, ep, g.HalfmoveClock, fullMove)
}

// MoveFromUCI parses a UCI move string ("e2e4") into a Move. Promotion
// suffixes are accepted and ignored: this engine always promotes to a
// queen, which is what it would have chosen anyway in almost every case.
func MoveFromUCI(s string) (Move, bool) {
	if len(s) < 4 {
		return Move{}, false
	}
	fileOf := func(c byte) int { return int(c - 'a') }
	rankOf := func(c byte) int { return int(c - '1') }
	m := Move{
		From: board.Sq{File: fileOf(s[0]), Rank: rankOf(s[1])},
		To:   board.Sq{File: fileOf(s[2]), Rank: rankOf(s[3])},
	}
	for _, sq := range []board.Sq{m.From, m.To} {
		if sq.File < 0 || sq.File > 7 || sq.Rank < 0 || sq.Rank > 7 {
			return Move{}, false
		}
	}
	return m, true
}

// UCI renders a move as a UCI string.
func (m Move) UCI() string {
	return fmt.Sprintf("%c%d%c%d", 'a'+m.From.File, m.From.Rank+1, 'a'+m.To.File, m.To.Rank+1)
}

// ParseFEN is the inverse of FEN: piece placement, side to move, castling
// rights, the en passant target and the halfmove clock.
func ParseFEN(s string) (*Game, error) {
	fields := strings.Fields(s)
	if len(fields) < 6 {
		return nil, fmt.Errorf("fen: want 6 fields, got %d in %q", len(fields), s)
	}

	ranks := strings.Split(fields[0], "/")
	if len(ranks) != 8 {
		return nil, fmt.Errorf("fen: want 8 ranks, got %d", len(ranks))
	}

	fromLetter := map[byte]board.PieceType{}
	for t, ch := range fenLetters {
		fromLetter[ch] = board.PieceType(t)
	}

	b := board.NewEmpty()
	for i, row := range ranks {
		rank := 7 - i // FEN starts at rank 8
		file := 0
		for j := 0; j < len(row); j++ {
			ch := row[j]
			if ch >= '1' && ch <= '8' {
				file += int(ch - '0')
				continue
			}
			color := board.Black
			lower := ch
			if ch >= 'A' && ch <= 'Z' {
				color = board.White
				lower = ch - 'A' + 'a'
			}
			pt, ok := fromLetter[lower]
			if !ok {
				return nil, fmt.Errorf("fen: unknown piece %q in rank %q", string(ch), row)
			}
			if file > 7 {
				return nil, fmt.Errorf("fen: rank %q overflows the board", row)
			}
			b.Place(board.Sq{File: file, Rank: rank}, board.Piece{Color: color, Type: pt})
			file++
		}
		if file != 8 {
			return nil, fmt.Errorf("fen: rank %q covers %d files, want 8", row, file)
		}
	}

	var turn board.Color
	switch fields[1] {
	case "w":
		turn = board.White
	case "b":
		turn = board.Black
	default:
		return nil, fmt.Errorf("fen: side to move %q is not w or b", fields[1])
	}

	var rights uint8
	for _, ch := range fields[2] {
		switch ch {
		case 'K':
			rights |= board.WhiteKingSide
		case 'Q':
			rights |= board.WhiteQueenSide
		case 'k':
			rights |= board.BlackKingSide
		case 'q':
			rights |= board.BlackQueenSide
		}
	}
	b.SetCastle(rights)

	if f := fields[3]; f != "-" && len(f) >= 2 {
		file, rank := int(f[0]-'a'), int(f[1]-'1')
		if file >= 0 && file < 8 && rank >= 0 && rank < 8 {
			b.SetEPSquare(board.Sq{File: file, Rank: rank}, true)
		}
	} else {
		b.SetEPSquare(board.Sq{}, false)
	}

	g := From(b, turn)
	if n, err := strconv.Atoi(fields[4]); err == nil {
		g.HalfmoveClock = n
	}
	return g, nil
}
