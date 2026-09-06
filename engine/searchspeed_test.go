package engine

import (
	"testing"
	"time"

	"chess/game"
)

// What does the adopted network cost the search?
//
// It is now part of the champion, so its inference cost is paid on every
// node of every move. This measures how much search the engine gives up
// for it, which is what an incremental accumulator (idea 11) would buy
// back.
func TestNetworkSearchCost(t *testing.T) {
	n, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network")
	}
	fens := []string{
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"r2q1rk1/pp1bbppp/2np1n2/4p3/2P1P3/2NP1N2/PP2BPPP/R1BQ1RK1 w - - 0 10",
		"r1bqkb1r/pp3ppp/2n1pn2/2pp4/3P4/2NBPN2/PPP2PPP/R1BQK2R w KQkq - 0 7",
	}
	for _, withNet := range []bool{false, true} {
		p := Strong(4)
		if withNet {
			p.HalfKP, p.HalfKPBlend = n, 0.45
		}
		t0 := time.Now()
		moves := 0
		for _, fen := range fens {
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 6; i++ {
				m, ok := PlayerPick(p, g)
				if !ok {
					break
				}
				g.ApplyMove(m.From, m.To)
				moves++
			}
		}
		el := time.Since(t0)
		label := "hand evaluation"
		if withNet {
			label = "with the network"
		}
		t.Logf("%-18s %d moves at depth 4 in %s, %.0f ms per move",
			label, moves, el.Truncate(time.Millisecond), float64(el.Milliseconds())/float64(moves))
	}
}
