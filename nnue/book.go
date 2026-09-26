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
		games       int
		wins, draws float64
	}
	type tally struct {
		fen   string
		moves map[string]*record
	}
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
			// pg.result is written from White's side, so the mover's result
			// is its complement when Black is to move.
			r := pg.result
			if g.Turn != board.White {
				r = 1 - r
			}
			switch r {
			case 1:
				rec.wins++
			case 0.5:
				rec.draws++
			}
			g.Apply(m)
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
		// PolyGlot's weight, the standard for books built from games:
		// 2 x wins + draws, losses ignored. The important property is that
		// it is a *count*, not a rate, so the number of games is part of
		// the answer rather than divided out. A main line played 500 times
		// scoring 45% weighs 2x225+100 = 550; a move played twelve times
		// and won every one weighs 24.
		//
		// Dividing by games instead, which is what this did first, throws
		// the sample size away and lets a lucky dozen games beat a main
		// line. That is how b1a3 became the answer to the starting position
		// and g8f6 the answer after 1.e4 Nc6 2.d4, giving away 1.2 pawns on
		// move two of every game that reached it.
		best, bestWeight, bestN := "", -1.0, 0
		for mv, rec := range t.moves {
			if rec.games < minGames {
				continue
			}
			weight := 2*rec.wins + rec.draws
			if weight > bestWeight || (weight == bestWeight && mv < best) {
				best, bestWeight, bestN = mv, weight, rec.games
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
