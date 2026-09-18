package main

import (
	"fmt"
	"sort"

	"chess/board"
	"chess/game"
)

// phase buckets a position by how much material is left, because an
// evaluation defect in the opening and one in a rook endgame are
// different bugs and averaging them hides both.
func phase(g *game.Game) string {
	men := len(g.Board.PiecesOf(board.White)) + len(g.Board.PiecesOf(board.Black))
	// 30 rather than 26: a position with 28 men can already have both
	// kings castled and every minor piece developed, and calling that an
	// opening puts real middlegame mistakes in the bucket nobody reads.
	switch {
	case men >= 30:
		return "opening"
	case men >= 15:
		return "middlegame"
	default:
		return "endgame"
	}
}

// disagreement is one position where the judge chose a different move.
type disagreement struct {
	fen      string
	phase    string
	ours     string
	theirs   string
	costPawn float64
}

// tally counts agreements per phase and keeps the worst disagreements.
type tally struct {
	seen   map[string]int
	agreed map[string]int
	worst  []disagreement
	keep   int
}

func newTally(keep int) *tally {
	return &tally{seen: map[string]int{}, agreed: map[string]int{}, keep: keep}
}

// add records one judged position. cost is how much the judge says our
// move gives up, in pawns, and is only meaningful when the moves differ.
func (t *tally) add(d disagreement, agreed bool) {
	t.seen[d.phase]++
	if agreed {
		t.agreed[d.phase]++
		return
	}
	t.worst = append(t.worst, d)
	sort.Slice(t.worst, func(i, j int) bool { return t.worst[i].costPawn > t.worst[j].costPawn })
	if len(t.worst) > t.keep {
		t.worst = t.worst[:t.keep]
	}
}

// report is the summary, ordered so the phases read in game order.
func (t *tally) report() string {
	out := "phase       positions  agreed  rate\n"
	total, totalAgreed := 0, 0
	for _, p := range []string{"opening", "middlegame", "endgame"} {
		n, a := t.seen[p], t.agreed[p]
		total, totalAgreed = total+n, totalAgreed+a
		rate := 0.0
		if n > 0 {
			rate = 100 * float64(a) / float64(n)
		}
		out += fmt.Sprintf("%-12s %8d %7d %5.1f%%\n", p, n, a, rate)
	}
	rate := 0.0
	if total > 0 {
		rate = 100 * float64(totalAgreed) / float64(total)
	}
	out += fmt.Sprintf("%-12s %8d %7d %5.1f%%\n", "all", total, totalAgreed, rate)
	return out
}
