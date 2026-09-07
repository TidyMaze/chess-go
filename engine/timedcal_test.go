package engine

import (
	"testing"
	"time"

	"chess/game"
)

// What does a time budget actually cost per move, against the fixed depth
// it is meant to be compared with? An A/B between them is only fair if
// the two spend the same wall clock, and the search stops between
// iterations so it can overrun by most of a final iteration.
func TestTimeBudgetCalibration(t *testing.T) {
	fens := []string{
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"r2q1rk1/pp1bbppp/2np1n2/4p3/2P1P3/2NP1N2/PP2BPPP/R1BQ1RK1 w - - 0 10",
		"r1bqkb1r/pp3ppp/2n1pn2/2pp4/3P4/2NBPN2/PPP2PPP/R1BQK2R w KQkq - 0 7",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	}
	measure := func(p Player) (float64, int) {
		total := time.Duration(0)
		moves, nodes := 0, 0
		for _, fen := range fens {
			g, _ := game.ParseFEN(fen)
			for i := 0; i < 5; i++ {
				t0 := time.Now()
				m, ok := PlayerPick(p, g)
				total += time.Since(t0)
				if !ok {
					break
				}
				nodes += LastSearchNodesValue()
				g.ApplyMove(m.From, m.To)
				moves++
			}
		}
		return float64(total.Microseconds()) / float64(moves) / 1000, nodes / moves
	}

	ref := Strong(4)
	ms, nodes := measure(ref)
	t.Logf("fixed depth 4:        %.1f ms per move, %d nodes", ms, nodes)
	for _, budget := range []int{8, 16, 32} {
		p := Strong(4)
		p.TimeBudget = time.Duration(budget) * time.Millisecond
		ms, nodes := measure(p)
		t.Logf("budget %2d ms:         %.1f ms per move, %d nodes", budget, ms, nodes)
	}
}
