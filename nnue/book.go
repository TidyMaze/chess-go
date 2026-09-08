package main

import (
	"fmt"
	"io"
	"sort"

	"chess/engine"
	"chess/game"
)

// BuildBookFromPGN writes an opening book of "FEN|move" lines from the
// moves people played in the games on r: for each position reached
// within maxPlies of the start, the most frequently played move, kept
// only if it was played in at least minGames games.
//
// This is the book the rules allow. The existing -extract-book took its
// moves from the Lichess evaluation dump, which are engine choices, and
// the rule is that only this engine's own search may score a position;
// a database of games is match history and is fine. Popularity is the
// signal: a move a thousand players chose is opening theory, a move
// three players chose is one of their mistakes.
func BuildBookFromPGN(r io.Reader, w io.Writer, maxPlies, minGames int) (int, error) {
	type tally struct {
		fen   string
		moves map[string]int
	}
	book := map[string]*tally{}
	games := make(chan pgnGame, 64)
	go readPGN(r, games)
	for pg := range games {
		g := game.New()
		for ply, san := range pg.moves {
			if ply >= maxPlies {
				break
			}
			m, ok := game.MoveFromSAN(g, san)
			if !ok {
				break
			}
			fen := g.FEN()
			key := engine.BookKey(fen)
			t := book[key]
			if t == nil {
				t = &tally{fen: fen, moves: map[string]int{}}
				book[key] = t
			}
			t.moves[m.UCI()]++
			g.ApplyMove(m.From, m.To)
		}
	}
	keys := make([]string, 0, len(book))
	for k := range book {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	written := 0
	for _, k := range keys {
		t := book[k]
		best, bestN := "", 0
		for mv, n := range t.moves {
			if n > bestN || (n == bestN && mv < best) {
				best, bestN = mv, n
			}
		}
		if bestN < minGames {
			continue
		}
		if _, err := fmt.Fprintf(w, "%s|%s\n", t.fen, best); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}
