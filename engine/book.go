package engine

import (
	"bufio"
	"os"
	"strings"

	"chess/game"
)

// A Book maps a position to a move that a deep search chose there.
//
// The engine currently reasons from move one, which spends its search on
// positions that are already settled theory and reaches the middlegame
// having found some of it and missed the rest. Every strong engine plays
// the opening from a book instead, and the moves here come from the same
// Lichess dump used for training: principal variations from searches at
// depth 46 to 58, which is roughly forty plies deeper than this engine
// will ever see.
//
// Positions are keyed on placement, side to move, castling rights and the
// en passant square, and deliberately not on the move counters: the same
// position reached by a different move order must hit the same entry.
type Book struct {
	moves map[string]string // position key -> UCI move
}

// BookKey is the part of a FEN that identifies a position for book
// purposes. The halfmove clock and fullmove number are dropped.
func BookKey(fen string) string {
	f := strings.Fields(fen)
	if len(f) < 4 {
		return strings.TrimSpace(fen)
	}
	return strings.Join(f[:4], " ")
}

// LoadBook reads "FEN|move" lines. A malformed line is skipped rather
// than fatal: a book is an optimisation, and a bad line in it must cost
// one position rather than the ability to play.
func LoadBook(path string) (*Book, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	b := &Book{moves: map[string]string{}}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.LastIndexByte(line, '|')
		if i < 0 {
			continue
		}
		fen, mv := strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
		if len(mv) < 4 {
			continue
		}
		b.moves[BookKey(fen)] = mv
	}
	return b, sc.Err()
}

func (b *Book) Len() int {
	if b == nil {
		return 0
	}
	return len(b.moves)
}

// Move returns the book move for a position, if there is one and it is
// legal there. The legality check is not paranoia: the book is built from
// an external file, and playing an illegal move from it would corrupt a
// game in a way that looks like an engine bug.
func (b *Book) Move(g *game.Game) (game.Move, bool) {
	if b == nil || len(b.moves) == 0 {
		return game.Move{}, false
	}
	uci, ok := b.moves[BookKey(g.FEN())]
	if !ok {
		return game.Move{}, false
	}
	want, ok := game.MoveFromUCI(uci)
	if !ok {
		return game.Move{}, false
	}
	for _, m := range g.AllLegalMoves(g.Turn) {
		if m.From == want.From && m.To == want.To {
			m.Promo = want.Promo
			return m, true
		}
	}
	return game.Move{}, false
}
