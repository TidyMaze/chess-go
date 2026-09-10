package engine

import (
	"math"
	"os"
	"sort"
	"testing"

	"chess/board"
	"chess/game"
)

// Why do matches take six times their arithmetic? Measure it: how long a
// game between two near-identical engines actually runs, and what the
// score looks like late in the ones that end level.
//
// The draw band was set to a tenth of a pawn, which is Fishtest's number
// for Stockfish's evaluation scale, not a measurement of ours. If our
// scores hover further from zero in a level position, the rule never
// fires and every drawn game runs to the ply cap.
func TestGameLengthAndLateScoreDistribution(t *testing.T) {
	if os.Getenv("MEASURE") == "" {
		t.Skip("set MEASURE=1")
	}
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network:", err)
	}
	oldWin, oldDraw := AdjudicateWinPawns, AdjudicateDrawPawns
	AdjudicateWinPawns, AdjudicateDrawPawns = 0, 0 // measure unadjudicated
	defer func() { AdjudicateWinPawns, AdjudicateDrawPawns = oldWin, oldDraw }()

	list, err := LoadOpeningsList("../openings.txt")
	if err != nil {
		t.Fatal(err)
	}
	var lengths []int
	var lateAbs []float64
	decisive := 0
	for i := 0; i < 12; i++ {
		g, err := game.ParseFEN(list[i*37%len(list)])
		if err != nil {
			continue
		}
		g.TrackRepetition = true
		a, b := Strong(4), Strong(4)
		a.HalfKP, a.HalfKPBlend = net, 0.45
		b.HalfKP, b.HalfKPBlend = net, 0.45
		b.MainSEE = true
		plies := 0
		var scores []float64
		tables := map[board.Color]*TranspositionTable{board.White: NewTranspositionTable(16), board.Black: NewTranspositionTable(16)}
		for plies = 0; plies < 250 && !g.IsOver(); plies++ {
			p := a
			if g.Turn == board.Black {
				p = b
			}
			m, sc, ok := p.pickScored(g, tables[g.Turn])
			if !ok {
				break
			}
			g.ApplyMove(m.From, m.To)
			scores = append(scores, math.Abs(sc))
		}
		lengths = append(lengths, plies)
		if g.IsCheckmate(g.Turn) || g.KingCaptured {
			decisive++
		} else {
			// The last twenty plies of a game that ended level: this is the
			// window the draw rule has to fire in.
			from := len(scores) - 20
			if from < 0 {
				from = 0
			}
			lateAbs = append(lateAbs, scores[from:]...)
		}
	}
	sort.Ints(lengths)
	sort.Float64s(lateAbs)
	pct := func(v []float64, p float64) float64 {
		if len(v) == 0 {
			return 0
		}
		return v[int(p*float64(len(v)-1))]
	}
	t.Logf("%d games, %d decisive; plies median %d, max %d", len(lengths), decisive,
		lengths[len(lengths)/2], lengths[len(lengths)-1])
	t.Logf("late |score| in level games: p10 %.3f  p25 %.3f  median %.3f  p75 %.3f  (n=%d)",
		pct(lateAbs, 0.10), pct(lateAbs, 0.25), pct(lateAbs, 0.50), pct(lateAbs, 0.75), len(lateAbs))
	t.Logf("fraction within the current 0.10 band: %.1f%%", 100*fractionUnder(lateAbs, 0.10))
	t.Logf("fraction within 0.30: %.1f%%   within 0.50: %.1f%%",
		100*fractionUnder(lateAbs, 0.30), 100*fractionUnder(lateAbs, 0.50))
}

func fractionUnder(v []float64, x float64) float64 {
	if len(v) == 0 {
		return 0
	}
	n := 0
	for _, s := range v {
		if s <= x {
			n++
		}
	}
	return float64(n) / float64(len(v))
}
