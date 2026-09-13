package main

import (
	"fmt"
	"io"
	"sort"

	"chess/board"
	"chess/engine"
	"chess/game"
)

// BuildBookFromPGN writes an opening book of "FEN|move" lines from the
// moves people played in the games on r: for each position reached within
// maxPlies of the start, the move that scored best for the side to move,
// kept only if it was played in at least minGames games by players rated
// minElo or better.
//
// This is the book the rules allow. The existing -extract-book took its
// moves from the Lichess evaluation dump, which are engine choices, and
// the rule is that only this engine's own search may score a position; a
// database of games is match history and is fine.
//
// Popularity alone is not enough, which was measured rather than assumed.
// Judged by this engine at depth 7, the purely most played book agreed
// with the engine 41.5% of the time and 8.5% of its moves lost more than
// half a pawn, the worst of them hanging a queen for a bishop because
// twenty weak players had grabbed it. So two things changed. Games below
// minElo are ignored, since the most played move among weak players is
// whatever was tempting. And among the moves that remain the winner is
// the one that actually scored, not the one played most often.
//
// The score is shrunk toward a draw by priorGames, so a move played the
// bare minimum of times cannot beat a main line on a lucky run: with
// twenty games of prior at even score, going 20 for 20 reads as 0.75
// rather than 1.0, while a move with three hundred games keeps almost all
// of its record.
func BuildBookFromPGN(r io.Reader, w io.Writer, maxPlies, minGames, minElo int) (int, error) {
	// Per move: how often it was played and what it scored for the side
	// that played it, so the winner can be the move that worked rather
	// than the move that was common.
	type record struct {
		games int
		score float64
	}
	type tally struct {
		fen   string
		moves map[string]*record
	}
	const priorGames = 20.0
	book := map[string]*tally{}
	games := make(chan pgnGame, 64)
	go readPGN(r, games)
	for pg := range games {
		if minElo > 0 && (pg.whiteElo < minElo || pg.blackElo < minElo) {
			continue
		}
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
				t = &tally{fen: fen, moves: map[string]*record{}}
				book[key] = t
			}
			rec := t.moves[m.UCI()]
			if rec == nil {
				rec = &record{}
				t.moves[m.UCI()] = rec
			}
			rec.games++
			// pg.result is written from White's side, so Black's score in
			// the same game is its complement.
			if g.Turn == board.White {
				rec.score += pg.result
			} else {
				rec.score += 1 - pg.result
			}
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
		best, bestRate, bestN := "", -1.0, 0
		for mv, rec := range t.moves {
			if rec.games < minGames {
				continue
			}
			rate := (rec.score + 0.5*priorGames) / (float64(rec.games) + priorGames)
			if rate > bestRate || (rate == bestRate && mv < best) {
				best, bestRate, bestN = mv, rate, rec.games
			}
		}
		if bestN == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "%s|%s\n", t.fen, best); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}
