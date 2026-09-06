package game

import (
	"strings"

	"chess/board"
)

// Standard Algebraic Notation, the move format PGN uses.
//
// The engine speaks UCI everywhere else (from-square, to-square), which is
// unambiguous and needs no board context. SAN is the opposite: "Nf3" names
// a destination and a piece type and leaves the origin to be worked out
// from the position, adding just enough disambiguation to be unique. So
// this resolves against the legal move list rather than parsing coordinates.
//
// Needed to learn from real games: PGN is what a games database ships, and
// a games database is the honest source of training positions. Position
// evaluations from an external engine are not, which is why the importer
// that uses this takes moves and results and nothing else.

// MoveFromSAN resolves one SAN token against a position.
//
// Returns false rather than guessing when the token matches no legal move
// or more than one: a game that fails to replay is skipped, and skipping a
// game is much cheaper than silently importing positions from a line that
// was never played.
func MoveFromSAN(g *Game, san string) (Move, bool) {
	s := strings.TrimSpace(san)
	// Check, mate and annotation marks carry no move information.
	s = strings.TrimRight(s, "+#?!")
	if s == "" {
		return Move{}, false
	}

	legal := g.AllLegalMoves(g.Turn)

	// Castling. PGN writes it with letter O; some producers use zero.
	if c := strings.ReplaceAll(s, "0", "O"); c == "O-O" || c == "O-O-O" {
		king := g.Board.KingSquare(g.Turn)
		wantFile := 6 // g-file, short
		if c == "O-O-O" {
			wantFile = 2 // c-file, long
		}
		for _, m := range legal {
			if m.From == king && m.To.Rank == king.Rank && m.To.File == wantFile {
				return m, true
			}
		}
		return Move{}, false
	}

	// Promotion suffix, "=Q" or bare "Q" after the square.
	promo := board.Queen
	if i := strings.IndexByte(s, '='); i >= 0 {
		if i+1 >= len(s) {
			return Move{}, false
		}
		p, ok := pieceFromSAN(s[i+1])
		if !ok {
			return Move{}, false
		}
		promo = p
		s = s[:i]
	}
	_ = promo // the engine auto-queens; recorded for clarity, see below

	// Leading piece letter. Absent means a pawn move.
	piece := board.Pawn
	if len(s) > 0 && s[0] >= 'A' && s[0] <= 'Z' {
		p, ok := pieceFromSAN(s[0])
		if !ok {
			return Move{}, false
		}
		piece = p
		s = s[1:]
	}

	s = strings.ReplaceAll(s, "x", "")
	if len(s) < 2 {
		return Move{}, false
	}

	// The last two characters are the destination; anything before them is
	// disambiguation, a file letter, a rank digit, or both.
	destStr := s[len(s)-2:]
	dest, ok := squareFromSAN(destStr)
	if !ok {
		return Move{}, false
	}
	hint := s[:len(s)-2]
	hintFile, hintRank := -1, -1
	for i := 0; i < len(hint); i++ {
		switch c := hint[i]; {
		case c >= 'a' && c <= 'h':
			hintFile = int(c - 'a')
		case c >= '1' && c <= '8':
			hintRank = int(c - '1')
		default:
			return Move{}, false
		}
	}

	var found Move
	matches := 0
	for _, m := range legal {
		if m.To != dest {
			continue
		}
		p, ok := g.Board.PieceAt(m.From)
		if !ok || p.Type != piece {
			continue
		}
		if hintFile >= 0 && m.From.File != hintFile {
			continue
		}
		if hintRank >= 0 && m.From.Rank != hintRank {
			continue
		}
		found, matches = m, matches+1
	}
	if matches != 1 {
		// Zero means the token does not describe a legal move here, more
		// than one means the PGN under-disambiguated. Either way the game
		// cannot be replayed faithfully, so refuse it.
		return Move{}, false
	}
	return found, true
}

func pieceFromSAN(c byte) (board.PieceType, bool) {
	switch c {
	case 'K':
		return board.King, true
	case 'Q':
		return board.Queen, true
	case 'R':
		return board.Rook, true
	case 'B':
		return board.Bishop, true
	case 'N':
		return board.Knight, true
	case 'P':
		return board.Pawn, true
	}
	return 0, false
}

func squareFromSAN(s string) (board.Sq, bool) {
	if len(s) != 2 || s[0] < 'a' || s[0] > 'h' || s[1] < '1' || s[1] > '8' {
		return board.Sq{}, false
	}
	return board.Sq{File: int(s[0] - 'a'), Rank: int(s[1] - '1')}, true
}
