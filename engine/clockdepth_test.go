package engine

import (
	"testing"
	"time"

	"chess/game"
)

// Against Stockfish capped at 2800 the engine reads -307 at 10 ms, -309 at
// 100 ms and -133 at 1 s. Ten times the thinking time bought nothing
// between the first two, which has two explanations: the search fails to
// convert time into depth down there, or the capped opponent converts it
// just as well as we do and only stops improving later.
//
// This measures our half of that. If depth climbs normally from 10 ms to
// 100 ms then the search is fine and the flat stretch belongs to the
// opponent's limiter.
func TestDepthReachedAcrossClocks(t *testing.T) {
	skipIfMachineBusy(t)
	if testing.Short() {
		t.Skip("timing measurement")
	}
	fens := []string{
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
	}
	prev := 0.0
	for _, ms := range []int{10, 100, 1000} {
		sumDepth, sumNodes := 0, 0
		for _, fen := range fens {
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			p := Strong(64)
			p.ApplyFeatures("lmp,deeplmp,rfp,scaledlmr,countermove,see,historymalus,lmrtwostep,nullgate,iir,improving,razoring")
			ev := evalForPlayer(p)
			ev.Table = NewTranspositionTable(22)
			ChooseMoveIterativeTimed(g, g.Turn, 64, ev, true, time.Duration(ms)*time.Millisecond)
			sumDepth += LastSearchDepth()
			sumNodes += LastSearchNodesValue()
		}
		mean := float64(sumDepth) / float64(len(fens))
		t.Logf("%5d ms: mean depth %.1f, mean nodes %d", ms, mean, sumNodes/len(fens))
		// Ten times the clock has to buy at least two plies. Below that
		// the search is not converting time into depth, which is a bug
		// in the iterative deepening or the time manager, not a property
		// of the position.
		if prev > 0 && mean < prev+2 {
			t.Errorf("%d ms reached mean depth %.1f, only %.1f deeper than the previous clock",
				ms, mean, mean-prev)
		}
		prev = mean
	}
}
