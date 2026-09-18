package engine

import (
	"fmt"
	"testing"
	"time"

	"chess/game"
)

// A 10 ms move must not be handicapped against a long one.
//
// The suspicion was a fixed per-move cost: an earlier reading showed 2,026
// nodes at 10 ms against 50,858 at 100 ms, 25 times the nodes for ten times
// the time. It was an artifact of allocating a fresh 4M entry table for each
// measurement, whose page faults are charged to whichever search runs first.
// With one warmed table, as real play has, throughput at 10 ms matches
// throughput at 320 ms.
func TestShortBudgetsAreNotHandicapped(t *testing.T) {
	skipIfMachineBusy(t)
	const fen = "r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9"
	// One table, warmed once. A fresh 4M entry table charges its page
	// faults to whoever runs first, which is the short budget, and real
	// play reuses the table from move to move.
	shared := NewTranspositionTable(22)
	warm, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}
	warmP := Strong(64)
	warmEv := evalForPlayer(warmP)
	warmEv.Table = shared
	ChooseMoveIterativeTimed(warm, warm.Turn, 64, warmEv, true, 300*time.Millisecond)

	rate := map[int]float64{}
	for _, ms := range []int{10, 20, 40, 80, 160, 320} {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		p := Strong(64)
		p.ApplyFeatures("lmp,deeplmp,rfp,scaledlmr,countermove,see,historymalus,lmrtwostep,nullgate,iir,improving,razoring")
		ev := evalForPlayer(p)
		ev.Table = shared
		start := time.Now()
		ChooseMoveIterativeTimed(g, g.Turn, 64, ev, true, time.Duration(ms)*time.Millisecond)
		el := time.Since(start)
		n := LastSearchNodesValue()
		knps := float64(n) / el.Seconds() / 1000
		fmt.Printf("%4d ms budget: %8d nodes, %7.2f ms elapsed, %6.0f knps\n",
			ms, n, float64(el.Microseconds())/1000, knps)
		rate[ms] = knps
	}
	// Half the long-budget rate is a wide margin: measured, the short
	// budget is the faster of the two. Anything below this means a fixed
	// per-move cost has appeared, which is worth hundreds of Elo at a
	// 10 ms control.
	if rate[10] < 0.5*rate[320] {
		t.Errorf("10 ms ran at %.0f knps against %.0f knps at 320 ms, so a per-move cost is eating the short budget",
			rate[10], rate[320])
	}
}
