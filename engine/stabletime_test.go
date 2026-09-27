package engine

import (
	"testing"
	"time"

	"chess/board"
	"chess/game"
)

func TestStableTimeIsAFeatureFlag(t *testing.T) {
	var off Player
	if off.StableTime || evalForPlayer(off).StableTime {
		t.Fatal("stabletime must be off by default")
	}
	var p Player
	p.ApplyFeatures("stabletime")
	p.HardBudget = 150 * time.Millisecond
	ev := evalForPlayer(p)
	if !p.StableTime || !ev.StableTime {
		t.Fatal("stabletime did not reach the Eval")
	}
	if ev.HardBudget != 150*time.Millisecond {
		t.Fatalf("HardBudget did not reach the Eval: %v", ev.HardBudget)
	}
}

// The hard limit is twice the soft target unless the caller lowers it. A
// caller can never raise it: a clock that allows three times the target
// on one move is the clock code's decision, not the search's.
func TestStableTimeHardLimit(t *testing.T) {
	const soft = 100 * time.Millisecond
	for _, c := range []struct {
		hard, want time.Duration
	}{
		{0, 2 * soft},
		{150 * time.Millisecond, 150 * time.Millisecond},
		{50 * time.Millisecond, 50 * time.Millisecond},
		{500 * time.Millisecond, 2 * soft},
	} {
		if got := stableHardLimit(soft, c.hard); got != c.want {
			t.Errorf("soft %v, caller hard %v: limit %v, want %v", soft, c.hard, got, c.want)
		}
	}
}

var (
	stableA = game.Move{From: board.Sq{File: 4, Rank: 1}, To: board.Sq{File: 4, Rank: 3}}
	stableB = game.Move{From: board.Sq{File: 3, Rank: 1}, To: board.Sq{File: 3, Rank: 3}}
)

// iters builds a history from moves and scores, each iteration taking a
// microsecond so the prediction rule stays out of the way.
func iters(moves []game.Move, scores []float64) []iterRecord {
	out := make([]iterRecord, len(moves))
	for i := range moves {
		out[i] = iterRecord{move: moves[i], score: scores[i], took: time.Microsecond}
	}
	return out
}

func repeatMove(m game.Move, n int) []game.Move {
	out := make([]game.Move, n)
	for i := range out {
		out[i] = m
	}
	return out
}

func flatScores(n int) []float64 { return make([]float64, n) }

func TestStableTimeFactor(t *testing.T) {
	if f := stableFactor(nil); f != 1.0 {
		t.Errorf("no history: factor %v, want 1.0", f)
	}
	if f := stableFactor(iters([]game.Move{stableA}, []float64{0})); f != 1.0 {
		t.Errorf("one iteration: factor %v, want 1.0", f)
	}
	if f := stableFactor(iters([]game.Move{stableA, stableB}, []float64{0, 0})); f <= 1.0 {
		t.Errorf("best move changed in the last iteration: factor %v, want above 1.0", f)
	}
	if f := stableFactor(iters([]game.Move{stableA, stableA}, []float64{0.5, 0.1})); f <= 1.0 {
		t.Errorf("score fell 0.4 pawns: factor %v, want above 1.0", f)
	}
	if f := stableFactor(iters([]game.Move{stableA, stableA}, []float64{0.5, 0.3})); f >= 1.0 {
		t.Errorf("same move, score fell only 0.2 pawns: factor %v, want below 1.0", f)
	}
	flip := make([]game.Move, 30)
	for i := range flip {
		flip[i] = stableA
		if i%2 == 1 {
			flip[i] = stableB
		}
	}
	if f := stableFactor(iters(flip, flatScores(30))); f != 2.0 {
		t.Errorf("best move flipping every iteration: factor %v, want the 2.0 ceiling", f)
	}
	if f := stableFactor(iters(repeatMove(stableA, 30), flatScores(30))); f != 0.6 {
		t.Errorf("best move stable for 30 iterations: factor %v, want the 0.6 floor", f)
	}
	// The factor recovers from instability once the move settles.
	settle := append(append([]game.Move{}, flip[:6]...), repeatMove(stableA, 20)...)
	if f := stableFactor(iters(settle, flatScores(26))); f != 0.6 {
		t.Errorf("unstable then stable for 20 iterations: factor %v, want 0.6", f)
	}
}

func TestStableTimeStopDecision(t *testing.T) {
	const soft = 100 * time.Millisecond
	hard := stableHardLimit(soft, 0)
	ms := func(f float64) time.Duration { return time.Duration(f * float64(time.Millisecond)) }
	rising := []float64{0.1, 0.2, 0.3, 0.4}
	withDrop := []float64{0.4, 0.2, 0.3, 0.3}
	flip := []game.Move{stableA, stableB, stableA, stableB, stableA}
	for _, c := range []struct {
		name    string
		elapsed time.Duration
		hard    time.Duration
		hist    []iterRecord
		stop    bool
	}{
		// At 55% of the target only the early stop can fire, since the
		// factor never goes below 0.6.
		{"4 same, rising, 55%", ms(55), hard, iters(repeatMove(stableA, 4), rising), true},
		{"4 same, rising, 45%", ms(45), hard, iters(repeatMove(stableA, 4), rising), false},
		{"4 same with a drop, 55%", ms(55), hard, iters(repeatMove(stableA, 4), withDrop), false},
		{"3 same, 55%", ms(55), hard, iters([]game.Move{stableB, stableA, stableA, stableA}, rising), false},
		// Neutral factor: the soft target itself.
		{"one iteration, 99%", ms(99), hard, iters([]game.Move{stableA}, []float64{0}), false},
		{"one iteration, 101%", ms(101), hard, iters([]game.Move{stableA}, []float64{0}), true},
		// An unstable best move buys up to twice the target.
		{"flipping, 190%", ms(190), hard, iters(flip, flatScores(5)), false},
		// A long stable run with a small drop blocks the early stop, so
		// what stops this one is the factor at its 0.6 floor.
		{"stable, small drop, 65%", ms(65), hard,
			iters(repeatMove(stableA, 10), []float64{0, 0, 0, 0, 0, 0, 0.3, 0.1, 0.1, 0.1}), true},
		// The hard limit wins over everything, including a lowered one.
		{"flipping, at hard", hard, hard, iters(flip, flatScores(5)), true},
		{"flipping, caller hard 120ms", ms(120), ms(120), iters(flip, flatScores(5)), true},
	} {
		if got := stableTimeStop(c.elapsed, soft, c.hard, c.hist); got != c.stop {
			t.Errorf("%s: stop %v, want %v", c.name, got, c.stop)
		}
	}
}

// An iteration predicted to run past the hard limit is not started: it
// would be cut off and its time wasted.
func TestStableTimeDoesNotStartAnIterationThatCannotFinish(t *testing.T) {
	const soft = 100 * time.Millisecond
	hard := stableHardLimit(soft, 0)
	ms := func(f float64) time.Duration { return time.Duration(f * float64(time.Millisecond)) }
	hist := func(prev, last time.Duration) []iterRecord {
		return []iterRecord{
			{move: stableA, score: 0, took: time.Millisecond},
			{move: stableB, score: 0, took: prev},
			{move: stableA, score: 0, took: last},
		}
	}
	// Iterations growing threefold: 20 then 60 ms, 85 ms spent, the next
	// one is predicted at 180 ms and would end at 265, past 200.
	if !stableTimeStop(ms(85), soft, hard, hist(ms(20), ms(60))) {
		t.Error("started an iteration predicted to end at 265ms on a 200ms hard limit")
	}
	// 10 then 30 ms, 45 ms spent: the next one should end near 135 ms.
	if stableTimeStop(ms(45), soft, hard, hist(ms(10), ms(30))) {
		t.Error("stopped although the next iteration is predicted to end at 135ms of 200")
	}
}

// fakeStableClock ticks step per reading, so every completed iteration of a
// stabletime search appears to take exactly step.
func fakeStableClock(t *testing.T, step time.Duration) {
	t.Helper()
	base := time.Now()
	n := 0
	stableNow = func() time.Time {
		n++
		return base.Add(time.Duration(n) * step)
	}
	t.Cleanup(func() { stableNow = time.Now })
}

// With the flag on the stop rule actually drives the search. A back-rank
// mate in one has the same best move and the same score from depth 1: with a
// clock ticking 10 ms per iteration on a 100 ms target, four identical
// iterations and more than 50 ms spent first happen after depth 6. A won
// position is no good here: winning a queen, the score swung 0.36 pawns
// between depths 5 and 6, which is instability by the rule.
func TestStableTimeStopsASettledSearchEarly(t *testing.T) {
	g, err := game.ParseFEN("6k1/5ppp/8/8/8/8/5PPP/R5K1 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	fakeStableClock(t, 10*time.Millisecond)
	p := Strong(4)
	p.Book, p.Threads = nil, 1
	p.TimeBudget = 100 * time.Millisecond
	p.ApplyFeatures("stabletime")
	m, ok := PlayerPick(p, g)
	if !ok || m.UCI() != "a1a8" {
		t.Fatalf("played %s, want a1a8", m.UCI())
	}
	if d := LastSearchDepth(); d != 6 {
		t.Errorf("stopped after depth %d, want 6", d)
	}
}

// Under a fixed depth there is no clock, so the flag changes nothing.
func TestStableTimeWithoutABudgetSearchesTheSameTree(t *testing.T) {
	g, err := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	if err != nil {
		t.Fatal(err)
	}
	nodes := func(features string) int {
		p := Strong(5)
		p.Book, p.Threads, p.TimeBudget = nil, 1, 0
		p.ApplyFeatures(features)
		SeedRandom(1)
		ResetNodes()
		PlayerPick(p, g)
		return TotalNodes()
	}
	if off, on := nodes(""), nodes("stabletime"); off != on {
		t.Errorf("fixed depth 5: %d nodes without stabletime, %d with it", off, on)
	}
}

// The search may run past the soft target but never meaningfully past the
// hard limit, which aborts it mid-iteration. Checked on the default limit
// and on one the caller lowered, the latter with a helper thread, which
// searches to the hard limit rather than stopping at the soft target.
func TestStableTimeNeverOverrunsTheHardLimit(t *testing.T) {
	skipIfMachineBusy(t)
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network:", err)
	}
	const soft = 20 * time.Millisecond
	fens := append(append([]string{}, correctnessPositions[3:8]...),
		"r1b2rk1/pp2bppp/2n1pn2/3p4/2qP4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"8/8/8/4k3/8/8/4P3/4K3 w - - 0 1",
	)
	for _, c := range []struct {
		callerHard time.Duration
		threads    int
	}{{0, 1}, {25 * time.Millisecond, 2}} {
		hard := stableHardLimit(soft, c.callerHard)
		worst := time.Duration(0)
		for _, fen := range fens {
			g, _ := game.ParseFEN(fen)
			p := Strong(1)
			p.HalfKP, p.HalfKPBlend, p.TimeBudget, p.HardBudget = net, 0.45, soft, c.callerHard
			p.Book, p.Threads = nil, c.threads
			p.ApplyFeatures("stabletime")
			tt := NewTranspositionTable(18)
			for i := 0; i < 3; i++ {
				start := time.Now()
				PlayerPickWith(p, g, tt)
				if el := time.Since(start); el > worst {
					worst = el
				}
			}
		}
		t.Logf("soft %v, hard %v: worst %v (%.0f%% of hard)", soft, hard, worst, 100*float64(worst)/float64(hard))
		if float64(worst) > 1.1*float64(hard) {
			if gap := worstPreemptionGap(50 * time.Millisecond); gap > 3*time.Millisecond {
				t.Skipf("machine is busy: worst %v, preemption gap %v", worst, gap)
			}
			t.Errorf("hard limit %v: a move took %v, %.0f%% of it", hard, worst, 100*float64(worst)/float64(hard))
		}
	}
}
