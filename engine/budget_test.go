package engine

import (
	"os"
	"testing"
	"time"

	"chess/game"
)

func TestLastSearchDepthReportsTheCompletedIteration(t *testing.T) {
	g, _ := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	PlayerPick(Strong(3), g)
	if d := LastSearchDepth(); d != 3 {
		t.Errorf("fixed depth 3 completed depth %d", d)
	}
}

// What a per-move budget buys: depth reached and share of the budget
// spent, on the profiling positions. PROFILE=1 to run.
func TestBudgetUsage(t *testing.T) {
	if os.Getenv("PROFILE") == "" {
		t.Skip("set PROFILE=1")
	}
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Fatal(err)
	}
	fens := []string{
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"r2q1rk1/1b1nbppp/p2ppn2/1p6/3NPP2/1BN1B3/PPPQ2PP/2KR3R w - - 0 12",
		"2rq1rk1/pp1bppbp/2np1np1/8/2BNP3/2N1BP2/PPPQ2PP/2KR3R w - - 0 11",
		"rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	}
	for _, budget := range []time.Duration{1000 * time.Millisecond, 3000 * time.Millisecond} {
		for _, fen := range fens {
			g, _ := game.ParseFEN(fen)
			p := Strong(5)
			p.HalfKP, p.HalfKPBlend, p.TimeBudget = net, 0.45, budget
			start := time.Now()
			PlayerPick(p, g)
			el := time.Since(start)
			t.Logf("budget %4dms  depth %2d  used %5dms (%3.0f%%)  %s",
				budget.Milliseconds(), LastSearchDepth(), el.Milliseconds(),
				100*float64(el)/float64(budget), fen[:20])
		}
	}
	for _, fen := range fens[:4] {
		g, _ := game.ParseFEN(fen)
		p := Strong(6)
		p.HalfKP, p.HalfKPBlend = net, 0.45
		start := time.Now()
		PlayerPick(p, g)
		t.Logf("fixed depth 6: %5dms  %s", time.Since(start).Milliseconds(), fen[:20])
	}
}

// A timed search must spend its budget. Predicting the next iteration at
// three times the last one stopped the search at 37-59% of the clock in
// middlegame positions, and reached the same depth a fixed depth 6 does,
// so the clock was buying nothing. It may also not overrun: the previous
// policy let one position run to 112%.
// At 10 ms a move the clock has to be read often enough to matter. The
// search reads it every 2048 nodes, three to five milliseconds of work,
// which is half a 10 ms budget in overrun and makes every 10 ms race and
// calibration measure the overrun as much as the engine.
func TestTenMillisecondBudgetIsRespected(t *testing.T) {
	skipIfMachineBusy(t)
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network:", err)
	}
	// 1 ms is allowed twice its budget: at that scale the fixed cost of a
	// search, resetting a context and reading the clock, is a real share.
	for _, c := range []struct {
		budget time.Duration
		allow  float64
	}{{10 * time.Millisecond, 1.3}, {time.Millisecond, 2.0}} {
		worst := time.Duration(0)
		for _, fen := range correctnessPositions[3:8] {
			g, _ := game.ParseFEN(fen)
			p := Strong(1)
			p.HalfKP, p.HalfKPBlend, p.TimeBudget = net, 0.45, c.budget
			tt := NewTranspositionTable(18)
			for i := 0; i < 3; i++ {
				start := time.Now()
				PlayerPickWith(p, g, tt)
				if el := time.Since(start); el > worst {
					worst = el
				}
			}
		}
		t.Logf("budget %v: worst %v (%.0f%%)", c.budget, worst, 100*float64(worst)/float64(c.budget))
		if float64(worst) > c.allow*float64(c.budget) {
			t.Errorf("a %v move took %v, %.0f%% of its budget", c.budget, worst, 100*float64(worst)/float64(c.budget))
		}
	}
}

// The clock is read more often the shorter the budget, and the mask stays a
// power of two minus one so the check remains a single AND.
func TestClockIsReadOftenEnoughForTheBudget(t *testing.T) {
	for _, c := range []struct {
		budget time.Duration
		mask   int
	}{
		{time.Millisecond, 15},
		{10 * time.Millisecond, 63},
		{49 * time.Millisecond, 63},
		{100 * time.Millisecond, 511},
		{time.Second, 2047},
	} {
		if got := clockCheckMask(c.budget); got != c.mask {
			t.Errorf("budget %v: mask %d, want %d", c.budget, got, c.mask)
		}
		if m := clockCheckMask(c.budget); m&(m+1) != 0 {
			t.Errorf("mask %d is not a power of two minus one", m)
		}
	}
}

func TestTimedSearchUsesItsBudget(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network:", err)
	}
	const budget = 800 * time.Millisecond
	total, worst := time.Duration(0), time.Duration(0)
	for _, fen := range correctnessPositions[3:8] {
		g, _ := game.ParseFEN(fen)
		p := Strong(4)
		p.HalfKP, p.HalfKPBlend, p.TimeBudget = net, 0.45, budget
		start := time.Now()
		PlayerPick(p, g)
		el := time.Since(start)
		total += el
		if el > worst {
			worst = el
		}
	}
	mean := total / 5
	t.Logf("budget %v: mean used %v (%.0f%%), worst %v (%.0f%%)", budget, mean,
		100*float64(mean)/float64(budget), worst, 100*float64(worst)/float64(budget))
	// Bounds are loose on purpose. The whole test suite runs its packages
	// in parallel, so a timed search here shares the machine with other
	// searches and a clock-based assertion has to survive that; the tight
	// numbers live in the measurement (89% mean, 106% worst, measured on a
	// quiet machine by TestBudgetUsage).
	if float64(mean) < 0.45*float64(budget) {
		t.Errorf("the search used only %.0f%% of its budget on average", 100*float64(mean)/float64(budget))
	}
	if float64(worst) > 2.0*float64(budget) {
		t.Errorf("the search overran its budget by %.0f%%", 100*float64(worst)/float64(budget)-100)
	}
}
