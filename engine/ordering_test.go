package engine

import (
	"testing"
	"time"

	"chess/game"
)

// How good is the move ordering?
//
// Alpha-beta's cost is decided by it: at a node where a cutoff exists,
// finding it with the first move searched costs one subtree, finding it
// with the fifth costs five. Strong engines cut on the first move about
// nine times in ten, which is why they reach twenty plies where this
// engine reaches seven. The whole Stockfish-ideas campaign came out
// neutral or negative, and late move pruning at -71 Elo says why: those
// techniques assume "late" means "bad", which is a statement about
// ordering, not about pruning.
//
// So measure it before adding an eighth pruning rule.
func TestMoveOrderingQualityIsMeasured(t *testing.T) {
	ResetOrderingStats()
	OrderingStats = true
	defer func() { OrderingStats = false }()

	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		PlayerScoreWith(Strong(6), g, nil)
	}
	rate, dist, total := OrderingReport()
	if total == 0 {
		t.Fatal("no cutoffs recorded: the instrument is not wired in")
	}
	t.Logf("cutoffs: %d, first move %.1f%%, distribution %v", total, 100*rate, dist)
	if rate <= 0 || rate >= 1 {
		t.Errorf("first-move cutoff rate %.3f is not a rate", rate)
	}
}

// The instrument must detect worse ordering, or it measures nothing.
// The table move is the strongest ordering signal there is, so taking the
// table away has to lower the rate.
func TestOrderingMeasurementDetectsWorseOrdering(t *testing.T) {
	measure := func(p Player) float64 {
		ResetOrderingStats()
		OrderingStats = true
		defer func() { OrderingStats = false }()
		for _, fen := range correctnessPositions {
			g, _ := game.ParseFEN(fen)
			PlayerScoreWith(p, g, nil)
		}
		rate, _, total := OrderingReport()
		if total == 0 {
			t.Fatal("no cutoffs")
		}
		return rate
	}
	withTable := Strong(5)
	withoutTable := Strong(5)
	withoutTable.TTBits = 0
	a, b := measure(withTable), measure(withoutTable)
	t.Logf("first-move cutoff rate: %.1f%% with the table, %.1f%% without", 100*a, 100*b)
	if b >= a {
		t.Errorf("removing the table move did not worsen the measured ordering: %.3f against %.3f", b, a)
	}
}

// Which of the implemented features actually improve the ordering?
//
// Ordering is measurable in seconds, where Elo takes an hour and a half,
// so this is the instrument to develop against. The campaign's features
// were judged on Elo alone and came out neutral; this asks the question
// they were built to answer.
func TestWhichFeaturesImproveTheOrdering(t *testing.T) {
	measure := func(mod func(*Player)) (float64, int64, int64) {
		p := Strong(6)
		mod(&p)
		ResetOrderingStats()
		ResetNodes()
		OrderingStats = true
		defer func() { OrderingStats = false }()
		for _, fen := range correctnessPositions {
			g, _ := game.ParseFEN(fen)
			PlayerScoreWith(p, g, nil)
		}
		rate, _, total := OrderingReport()
		return rate, total, int64(TotalNodes())
	}
	base, baseCut, baseNodes := measure(func(*Player) {})
	t.Logf("%-28s first move %.1f%%  cutoffs %6d  nodes %8d", "baseline", 100*base, baseCut, baseNodes)
	for _, c := range []struct {
		name string
		mod  func(*Player)
	}{
		{"SEE-ordered captures", func(p *Player) { p.MainSEE = true }},
		{"countermoves", func(p *Player) { p.Countermoves = true }},
		{"logarithmic reductions", func(p *Player) { p.ScaledLMR = true }},
		{"null-move gate", func(p *Player) { p.NullGate = true }},
		{"internal iterative reduction", func(p *Player) { p.IIR = true }},
		{"late move pruning", func(p *Player) { p.LMP = true }},
		{"deep reverse futility", func(p *Player) { p.DeepRFP = true }},
		{"everything", func(p *Player) {
			p.MainSEE, p.Countermoves, p.ScaledLMR = true, true, true
			p.NullGate, p.IIR = true, true
		}},
	} {
		rate, cut, nodes := measure(c.mod)
		t.Logf("%-28s first move %.1f%% (%+.1f)  cutoffs %6d  nodes %8d (%+.0f%%)",
			c.name, 100*rate, 100*(rate-base), cut, nodes,
			100*(float64(nodes)/float64(baseNodes)-1))
	}
}

// Do the node savings convert into depth under a clock?
//
// Fewer nodes only helps if the time saved buys plies. An ordering rule
// that removes 43% of nodes but costs more per node than it saves buys
// nothing, and the search's own node counter cannot see that: see()
// clones the board on every call.
func TestNodeSavingsConvertToDepth(t *testing.T) {
	if testing.Short() {
		t.Skip("plays timed searches")
	}
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network:", err)
	}
	fens := correctnessPositions[3:8]
	measure := func(name string, mod func(*Player)) {
		totalDepth, totalNodes := 0, 0
		start := time.Now()
		for _, fen := range fens {
			g, _ := game.ParseFEN(fen)
			p := Strong(5)
			p.HalfKP, p.HalfKPBlend, p.TimeBudget = net, 0.45, time.Second
			mod(&p)
			ResetNodes()
			PlayerPick(p, g)
			totalDepth += LastSearchDepth()
			totalNodes += TotalNodes()
		}
		el := time.Since(start)
		t.Logf("%-22s mean depth %.1f  nodes %8d  %.0f knodes/s",
			name, float64(totalDepth)/float64(len(fens)), totalNodes,
			float64(totalNodes)/el.Seconds()/1000)
	}
	measure("baseline", func(*Player) {})
	measure("SEE ordering only", func(p *Player) { p.MainSEE = true })
	measure("the whole stack", func(p *Player) {
		p.MainSEE, p.Countermoves, p.ScaledLMR = true, true, true
		p.NullGate, p.IIR = true, true
	})
}

// TestHistoryAgingDecaysHistory verifies that aging halves history values and
// produces legal moves across test positions.
func TestHistoryAgingDecaysHistory(t *testing.T) {
	c := searchCtxPool.Get().(*searchCtx)
	defer searchCtxPool.Put(c)
	c.reset()
	c.history[0][10][20] = 100
	c.history[1][5][15] = 50
	c.ageHistory()
	if c.history[0][10][20] != 50 || c.history[1][5][15] != 25 {
		t.Errorf("history values not halved: got %d and %d", c.history[0][10][20], c.history[1][5][15])
	}

	for _, fen := range correctnessPositions[:6] {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		p := Strong(5)
		p.HistoryAging = true
		m, ok := PlayerPick(p, g)
		if !ok || m.From == m.To {
			t.Errorf("history aging failed to pick move on %s", fen)
		}
	}
}

