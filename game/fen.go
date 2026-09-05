package game

import (
	"fmt"
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
// Castling rights and en-passant are always reported as unavailable
// because this engine does not implement either rule: claiming rights it
// cannot exercise would let the opponent search lines that are not
// actually reachable here.
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
	fullMove := g.HalfmoveClock/2 + 1
	return fmt.Sprintf("%s %s - - %d %d", sb.String(), side, g.HalfmoveClock, fullMove)
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
