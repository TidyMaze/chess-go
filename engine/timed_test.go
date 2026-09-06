package engine

import (
	"testing"
	"time"

	"chess/board"
	"chess/game"
)

// A time budget must actually bound the search, and must still return a
// legal move. A search that overruns its budget is not a time control,
// and one that returns nothing loses on the spot.
func TestTimeBudgetIsRespected(t *testing.T) {
	g, err := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range []time.Duration{20 * time.Millisecond, 60 * time.Millisecond} {
		p := Strong(4)
		p.TimeBudget = budget
		t0 := time.Now()
		m, ok := PlayerPick(p, g)
		el := time.Since(t0)
		if !ok {
			t.Fatalf("budget %s: no move returned", budget)
		}
		legal := false
		for _, lm := range g.AllLegalMoves(g.Turn) {
			if lm.From == m.From && lm.To == m.To {
				legal = true
			}
		}
		if !legal {
			t.Errorf("budget %s: returned an illegal move %s", budget, m.UCI())
		}
		// Generous: the search stops between iterations, so it can overrun
		// by up to one iteration. What must not happen is running for many
		// multiples of the budget.
		if el > 5*budget {
			t.Errorf("budget %s: took %s, more than five times the budget", budget, el)
		}
		t.Logf("budget %s: took %s, played %s", budget, el.Truncate(time.Millisecond), m.UCI())
	}
}

// More time must buy more search, or the budget is not doing anything.
func TestMoreTimeSearchesDeeper(t *testing.T) {
	g, _ := game.ParseFEN("r2q1rk1/pp1bbppp/2np1n2/4p3/2P1P3/2NP1N2/PP2BPPP/R1BQ1RK1 w - - 0 10")
	nodes := func(budget time.Duration) int {
		p := Strong(4)
		p.TimeBudget = budget
		PlayerPick(p, g)
		return LastSearchNodes
	}
	small, large := nodes(10*time.Millisecond), nodes(200*time.Millisecond)
	t.Logf("10ms searched %d nodes, 200ms searched %d", small, large)
	if large <= small {
		t.Errorf("200ms searched %d nodes and 10ms searched %d: the budget is ignored", large, small)
	}
	_ = board.White
}

// A trivial position must still respect the budget. Early iterations there
// cost microseconds, so a purely predictive stop rule never fires and the
// search deepens until one iteration explodes; two games out of 500 hung
// on this before the elapsed check was added.
func TestTimeBudgetStopsInATrivialPosition(t *testing.T) {
	for _, fen := range []string{
		"8/8/8/4k3/8/8/4P3/4K3 w - - 0 1",
		"8/8/8/3k4/8/8/8/K7 w - - 0 1",
		"8/8/4k3/8/8/8/8/K6R w - - 0 1",
	} {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		p := Strong(4)
		p.TimeBudget = 20 * time.Millisecond
		done := make(chan time.Duration, 1)
		go func() {
			t0 := time.Now()
			PlayerPick(p, g)
			done <- time.Since(t0)
		}()
		select {
		case el := <-done:
			if el > 2*time.Second {
				t.Errorf("%s: took %s for a 20ms budget", fen, el)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: still searching after 10s on a 20ms budget", fen)
		}
	}
}
