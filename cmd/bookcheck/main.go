// Command bookcheck asks this engine what it thinks of the opening book's
// moves.
//
// The book takes the most played move per position from a database of
// human games, with no rating filter and no regard for who won, so being
// in the book means a move was popular, not that it was good. The case on
// record for the book is entirely about clock: 16 plies played instantly
// instead of searched. Nothing had measured whether those 16 plies are
// worse than what the engine would have chosen on its own, which is what
// this measures.
//
// The judge is this engine's own fixed depth search, never an outside
// evaluation, so the answer is "worse by our own reckoning" rather than
// "worse according to somebody else's engine".
//
// Usage:
//
//	bookcheck -champion champion_bot.json -book games_book_full.txt -depth 8 -every 40
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"chess/engine"
	"chess/game"
)

// entry is one "FEN|move" line of the book.
type entry struct {
	fen  string
	move string
}

// readBook parses the book file. The format is deliberately just a
// position and a reply: no score, no depth, no principal variation, since
// a book carrying those would be an outside engine's opinion.
func readBook(path string) ([]entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fen, mv, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		out = append(out, entry{fen: fen, move: mv})
	}
	return out, sc.Err()
}

// verdict is what the engine thinks of one book move, in pawns lost
// against the move the engine would have played itself.
type verdict struct {
	entry
	engineMove string
	loss       float64
	agreed     bool
}

// judge scores the book's move against the engine's own choice from the
// same position, both searched to the same total depth. The book move is
// scored by playing it and searching the reply one ply shallower, then
// negating, which is what the position is worth to us after committing to
// it.
func judge(p engine.Player, e entry, depth int) (verdict, bool) {
	g, err := game.ParseFEN(e.fen)
	if err != nil {
		return verdict{}, false
	}
	best, bestScore, ok := engine.PlayerPickScored(atDepth(p, depth), g)
	if !ok || math.IsNaN(bestScore) {
		return verdict{}, false
	}
	v := verdict{entry: e, engineMove: best.UCI()}
	if best.UCI() == e.move {
		v.agreed = true
		return v, true
	}
	bm, ok := game.MoveFromUCI(e.move)
	if !ok {
		return verdict{}, false
	}
	child, err := game.ParseFEN(e.fen)
	if err != nil {
		return verdict{}, false
	}
	// ApplyMove reports nothing, so a move the book filed under the wrong
	// position would be applied silently. Checking it against the legal
	// moves first keeps an illegal entry out of the average instead of
	// scoring whatever position it produced.
	if !isLegal(child, bm) {
		return verdict{}, false
	}
	child.Apply(bm)
	// After our move it is the opponent to move, so their best score is
	// ours negated.
	_, reply, ok := engine.PlayerPickScored(atDepth(p, depth-1), child)
	if !ok || math.IsNaN(reply) {
		return verdict{}, false
	}
	v.loss = bestScore - (-reply)
	return v, true
}

// isLegal reports whether m is among the moves actually available, so the
// book cannot smuggle an illegal entry into the measurement.
func isLegal(g *game.Game, m game.Move) bool {
	for _, legal := range g.AllLegalMoves(g.Turn) {
		if legal.From == m.From && legal.To == m.To {
			return true
		}
	}
	return false
}

// atDepth makes a fixed depth, book free, deterministic version of the
// player. The book has to be cleared or the engine would answer with the
// very move being judged, and the search has to be iterative because the
// fixed depth path returns no score at all.
func atDepth(p engine.Player, depth int) engine.Player {
	p.Book = nil
	p.Depth = depth
	p.Iterative = true
	p.TimeBudget = 0
	p.Threads = 1
	return p
}

func main() {
	championPath := flag.String("champion", "champion_bot.json", "the evaluation that judges the book")
	bookPath := flag.String("book", "games_book_full.txt", "book to check")
	depth := flag.Int("depth", 8, "search depth for the judgement")
	every := flag.Int("every", 40, "check every Nth book position, so a run is reproducible without sampling at random")
	max := flag.Int("max", 400, "stop after this many positions (0 for all)")
	blunder := flag.Float64("blunder", 0.5, "a book move losing more than this many pawns is counted as a mistake")
	flag.Parse()

	p, err := engine.ReadChampion(*championPath).PlayerOrError()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bookcheck: champion:", err)
		os.Exit(1)
	}
	entries, err := readBook(*bookPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bookcheck: book:", err)
		os.Exit(1)
	}
	fmt.Printf("book %s: %d positions, judging every %d at depth %d\n",
		*bookPath, len(entries), *every, *depth)

	var losses []float64
	agreed, judged, skipped := 0, 0, 0
	var worst verdict
	for i := 0; i < len(entries); i += *every {
		if *max > 0 && judged >= *max {
			break
		}
		v, ok := judge(p, entries[i], *depth)
		if !ok {
			skipped++
			continue
		}
		judged++
		if v.agreed {
			agreed++
		}
		losses = append(losses, v.loss)
		if v.loss > worst.loss {
			worst = v
		}
	}
	if judged == 0 {
		fmt.Fprintln(os.Stderr, "bookcheck: nothing could be judged")
		os.Exit(1)
	}

	sort.Float64s(losses)
	var sum float64
	mistakes := 0
	for _, l := range losses {
		sum += l
		if l > *blunder {
			mistakes++
		}
	}
	fmt.Printf("\njudged %d positions (%d skipped)\n", judged, skipped)
	fmt.Printf("the engine would have played the book move        %5.1f%% of the time\n",
		100*float64(agreed)/float64(judged))
	fmt.Printf("mean loss against the engine's own choice          %+.3f pawns\n", sum/float64(judged))
	fmt.Printf("median loss                                        %+.3f pawns\n", losses[len(losses)/2])
	fmt.Printf("90th percentile loss                               %+.3f pawns\n", losses[len(losses)*9/10])
	fmt.Printf("book moves losing more than %.2f pawns             %d (%.1f%%)\n",
		*blunder, mistakes, 100*float64(mistakes)/float64(judged))
	if worst.loss > 0 {
		fmt.Printf("worst: %s\n  book played %s, engine wanted %s, losing %.2f pawns\n",
			worst.fen, worst.move, worst.engineMove, worst.loss)
	}
}
