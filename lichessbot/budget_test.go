package lichessbot

import (
	"fmt"
	"math"
	"os"
	"testing"
	"time"
)

// MoveTimeBudget is what clockaudit scores finished games against, so it
// must keep giving the numbers it gave before the opponent's clock and the
// book entered the rule the bot plays by. These are its outputs on a grid,
// taken before that change; a row that moves means an audit of an old game
// silently changes meaning.
func TestMoveTimeBudgetIsUnchangedOnAGridOfClocks(t *testing.T) {
	incs := [7]int64{0, 1000, 2000, 3000, 5000, 10000, 20000}
	grid := []struct {
		remain int64
		wantMS [7]int64
	}{
		{-5, [7]int64{0, 0, 0, 0, 0, 0, 0}},
		{0, [7]int64{0, 0, 0, 0, 0, 0, 0}},
		{1, [7]int64{50, 50, 50, 50, 50, 50, 50}},
		{7, [7]int64{50, 50, 50, 50, 50, 50, 50}},
		{100, [7]int64{50, 50, 50, 50, 50, 50, 50}},
		{250, [7]int64{50, 50, 50, 50, 50, 50, 50}},
		{400, [7]int64{50, 200, 200, 200, 200, 200, 200}},
		{1000, [7]int64{50, 366, 800, 800, 800, 800, 800}},
		{3000, [7]int64{50, 400, 900, 1400, 2400, 2800, 2800}},
		{5000, [7]int64{50, 433, 933, 1433, 2433, 4800, 4800}},
		{15000, [7]int64{50, 616, 1100, 1600, 2600, 5100, 10100}},
		{30000, [7]int64{200, 1116, 1450, 1850, 2850, 5350, 10350}},
		{60000, [7]int64{575, 2116, 2450, 2783, 3450, 5850, 10850}},
		{120000, [7]int64{1325, 4116, 4450, 4783, 5450, 7116, 11850}},
		{180000, [7]int64{2075, 6116, 6450, 6783, 7450, 9116, 12850}},
		{300000, [7]int64{3575, 10116, 10450, 10783, 11450, 13116, 15000}},
		{600000, [7]int64{7325, 15000, 15000, 15000, 15000, 15000, 15000}},
		{750000, [7]int64{9200, 15000, 15000, 15000, 15000, 15000, 15000}},
		{1200000, [7]int64{14825, 24000, 24000, 24000, 24000, 24000, 24000}},
		{1800000, [7]int64{22325, 30000, 30000, 30000, 30000, 30000, 30000}},
		{3600000, [7]int64{30000, 30000, 30000, 30000, 30000, 30000, 30000}},
	}
	for _, row := range grid {
		for i, inc := range incs {
			want := time.Duration(row.wantMS[i]) * time.Millisecond
			if got := MoveTimeBudget(row.remain, inc); got != want {
				t.Errorf("MoveTimeBudget(%d, %d) = %v, want %v as before", row.remain, inc, got, want)
			}
		}
	}
}

// within reports whether got is want milliseconds, to the microsecond the
// new rule rounds to.
func within(got time.Duration, wantMS float64) bool {
	return math.Abs(float64(got)-wantMS*float64(time.Millisecond)) <= float64(time.Microsecond)
}

// A 3+2 clock, level with the opponent, where no cap binds: the rule
// before any boost gives 1000 + (180000 - 12000) / 30 - 150 = 6450 ms,
// which the old function also returns there.
const evenBaseMS = 6450.0

func evenClock(k int) Clock {
	return Clock{OurMS: 180000, OppMS: 180000, IncMS: 2000, OppIncMS: 2000, MovesOutOfBook: k}
}

// The first moves out of book are where a game is decided while the clock
// is still full, so they get more of it: 1.8x on the first, falling by 0.08
// a move to nothing extra from the tenth on.
func TestTheFirstMovesOutOfBookGetABoostThatDecaysToNothing(t *testing.T) {
	for _, c := range []struct {
		k     int
		boost float64
	}{
		{-1, 1.0}, {0, 1.8}, {1, 1.72}, {5, 1.4}, {9, 1.08}, {10, 1.0}, {40, 1.0},
	} {
		if got := outOfBookBoost(c.k); math.Abs(got-c.boost) > 1e-9 {
			t.Errorf("outOfBookBoost(%d) = %v, want %v", c.k, got, c.boost)
		}
		if c.k < 0 {
			continue
		}
		want := evenBaseMS * c.boost
		if got := MoveBudget(evenClock(c.k)); !within(got, want) {
			t.Errorf("MoveBudget at 3+2, %d moves out of book = %v, want %.1fms", c.k, got, want)
		}
	}
}

// The opponent's clock matters: with them short of time, a deeper think
// costs nothing that matters; with us short, every second is worth more
// to us than to them. The square root keeps a 4:1 gap from quadrupling the
// spend, and the clamp keeps any gap from doing more than 1.4x or 0.7x.
func TestTheOpponentClockScalesTheBudget(t *testing.T) {
	for _, c := range []struct {
		name         string
		ourMS, oppMS int64
		factor       float64
	}{
		{"we have a quarter of their time", 180000, 720000, 0.7},
		{"level clocks", 180000, 180000, 1.0},
		{"they have a quarter of ours", 180000, 45000, 1.4},
		{"a gap small enough not to be clamped", 121000, 100000, 1.1},
		{"no opponent time reported", 180000, 0, 1.4},
	} {
		if got := clockCompensation(c.ourMS, c.oppMS); math.Abs(got-c.factor) > 1e-9 {
			t.Errorf("%s: clockCompensation(%d, %d) = %v, want %v", c.name, c.ourMS, c.oppMS, got, c.factor)
		}
	}
	for _, c := range []struct {
		r      float64
		oppMS  int64
		wantMS float64
	}{
		{0.25, 720000, evenBaseMS * 0.7},
		{1, 180000, evenBaseMS},
		{4, 45000, evenBaseMS * 1.4},
	} {
		clock := evenClock(10)
		clock.OppMS = c.oppMS
		if got := MoveBudget(clock); !within(got, c.wantMS) {
			t.Errorf("MoveBudget at r=%v = %v, want %.1fms", c.r, got, c.wantMS)
		}
	}
}

// Both boosts multiply the base and are then held by every cap the old
// rule had. Here 6450 x 1.8 x 1.4 = 16254 ms, over the 15 s ceiling a 3
// minute clock allows; on a half hour clock the 30 s cap holds.
func TestTheOldCapsStillHoldAfterTheBoosts(t *testing.T) {
	clock := evenClock(0)
	clock.OppMS = 45000
	if got := MoveBudget(clock); got != 15*time.Second {
		t.Errorf("boosted 3+2 budget = %v, want the 15s ceiling", got)
	}
	long := Clock{OurMS: 1800000, OppMS: 100000, IncMS: 20000, OppIncMS: 20000}
	if got := MoveBudget(long); got != 30*time.Second {
		t.Errorf("boosted 30+20 budget = %v, want the 30s cap", got)
	}
}

// On a short clock the safety rules decide, not the boosts: a boost that
// asked for 1 s with 3 s left would be a flag waiting for one slow search.
func TestTheSafetyCapsWinOnALowClock(t *testing.T) {
	for _, c := range []struct {
		name   string
		clock  Clock
		wantMS float64
	}{
		// 2+1 with 3 s left and the opponent flagging: 400 ms of base,
		// boosted to 1008, cut to an eighth of the clock.
		{"3s left at 2+1, both boosts", Clock{OurMS: 3000, OppMS: 100, IncMS: 1000, OppIncMS: 1000}, 375},
		// Without an increment the base is already under zero there: the
		// 50 ms floor still lets a move be played.
		{"1s left at 1+0", Clock{OurMS: 1000, OppMS: 60000}, 50},
		// Below 400 ms an eighth is less than the floor, and the eighth wins.
		{"300ms left", Clock{OurMS: 300, OppMS: 1000, IncMS: 1000}, 37.5},
		{"1ms left", Clock{OurMS: 1, OppMS: 1000}, 0.125},
	} {
		if got := MoveBudget(c.clock); !within(got, c.wantMS) {
			t.Errorf("%s: MoveBudget = %v, want %.3fms", c.name, got, c.wantMS)
		}
	}
	for _, ms := range []int64{0, -5} {
		if got := MoveBudget(Clock{OurMS: ms, OppMS: 60000}); got != 0 {
			t.Errorf("MoveBudget with %dms reported = %v, want 0 so the caller falls back", ms, got)
		}
	}
}

// reserveMS is the part of the clock the rule promises never to spend,
// written out here so the promise is checked against its definition and
// not against the code that keeps it.
func reserveMS(remain, inc int64) float64 {
	return math.Min(float64(2000+5*inc), float64(remain)/2)
}

// Every cap, on every clock, whatever the boosts ask for.
func TestMoveBudgetNeverBreaksASafetyRule(t *testing.T) {
	remains := []int64{1, 50, 250, 400, 1000, 3000, 5000, 15000, 60000, 120000, 300000, 1200000, 3600000}
	incs := []int64{0, 1000, 3000, 10000, 20000}
	opps := []int64{0, 1, 1000, 60000, 3600000}
	for _, remain := range remains {
		for _, inc := range incs {
			for _, opp := range opps {
				for k := 0; k <= 10; k++ {
					c := Clock{OurMS: remain, OppMS: opp, IncMS: inc, OppIncMS: inc, MovesOutOfBook: k}
					got := MoveBudget(c)
					ms := float64(got) / float64(time.Millisecond)
					switch {
					case got <= 0:
						t.Errorf("%+v: budget %v, a clocked move needs a positive one", c, got)
					case ms > float64(remain)/8:
						t.Errorf("%+v: budget %v is over an eighth of the clock", c, got)
					case ms > float64(remain)-reserveMS(remain, inc):
						t.Errorf("%+v: budget %v spends into the reserve", c, got)
					case ms > math.Max(float64(remain)/maxBudgetDivisor, minMaxBudgetMs):
						t.Errorf("%+v: budget %v is over the scaled ceiling", c, got)
					case got > 30*time.Second:
						t.Errorf("%+v: budget %v is over the 30s cap", c, got)
					}
				}
			}
		}
	}
}

// opponent is how the other side's clock moves in a simulated game.
type opponent struct {
	name string
	// next is their clock after they reply, given ours and theirs.
	next func(ourMS, oppMS, inc int64) int64
}

func opponentsToSimulate() []opponent {
	return []opponent{
		// Moves instantly: their clock only ever gains the increment, so
		// ours falls behind theirs and the rule has to spend less.
		{"instant", func(_, opp, inc int64) int64 { return opp + inc }},
		// Always on a quarter of our time: the rule spends 1.4x for the
		// whole game, which is the most the compensation can ask for.
		{"short of time", func(our, _, _ int64) int64 { return max(our/4, 1) }},
	}
}

// simulateMoveBudget plays a whole game through the rule the bot plays by,
// every move of it boosted as if the book had just run out on move one,
// with the same pessimistic overrun and round trip as simulateClock. It
// returns the lowest our clock got and the first move, if any, whose
// budget reached into the reserve.
func simulateMoveBudget(c control, opp opponent) (lowestMS int64, reserveBreach string) {
	const (
		overrunPercent = 110
		moveOverheadMS = 300
	)
	our, theirs := c.start, c.start
	lowest := our
	for i := 0; i < c.moves; i++ {
		clock := Clock{OurMS: our, OppMS: theirs, IncMS: c.inc, OppIncMS: c.inc, MovesOutOfBook: i}
		budget := moveBudget(clock, newOverheadEstimate())
		if reserveBreach == "" && float64(our)-float64(budget)/float64(time.Millisecond) < reserveMS(our, c.inc) {
			reserveBreach = fmt.Sprintf("move %d: budget %v with %dms left", i+1, budget, our)
		}
		used := budget.Milliseconds()*overrunPercent/100 + moveOverheadMS
		our -= min(used, our)
		lowest = min(lowest, our)
		if our <= 0 {
			return 0, reserveBreach
		}
		our += c.inc
		theirs = opp.next(our, theirs, c.inc)
	}
	return lowest, reserveBreach
}

// The case the change was asked to survive: 120 moves of 2+1 against an
// opponent who answers instantly, the boost spent on the first ten, and
// the clock must never fall and the budget must never reach the reserve.
func TestA2Plus1GameOf120MovesAgainstAnInstantOpponentNeverFlags(t *testing.T) {
	c := control{name: "bullet 2+1", start: 120000, inc: 1000, moves: 120}
	lowest, breach := simulateMoveBudget(c, opponentsToSimulate()[0])
	if lowest <= 0 {
		t.Errorf("flagged within %d moves of 2+1 against an instant opponent", c.moves)
	}
	if breach != "" {
		t.Errorf("the budget reached into the reserve: %s", breach)
	}
	if os.Getenv("CLOCKSIM") != "" {
		fmt.Printf("2+1 x120 vs instant: lowest %.1fs\n", float64(lowest)/1000)
	}
}

// And the same for every control the bot accepts, against an opponent who
// gives nothing back and one who keeps the compensation at its maximum.
// The floors the old rule is held to in clocksim_test.go still apply:
// the boosts are meant to move time between moves, not to spend the
// margin that keeps a slow search from flagging.
func TestTheNewRuleKeepsTheClockUsableInEveryControlWePlay(t *testing.T) {
	for _, c := range controlsWePlay() {
		for _, opp := range opponentsToSimulate() {
			lowest, breach := simulateMoveBudget(c, opp)
			if os.Getenv("CLOCKSIM") != "" {
				fmt.Printf("%-26s vs %-13s lowest %6.1fs (floor %.1fs)\n",
					c.name, opp.name, float64(lowest)/1000, float64(c.floorMS)/1000)
			}
			if lowest < c.floorMS {
				t.Errorf("%s vs %s: clock fell to %.1fs over %d moves, must stay above %.1fs",
					c.name, opp.name, float64(lowest)/1000, c.moves, float64(c.floorMS)/1000)
			}
			if breach != "" {
				t.Errorf("%s vs %s: the budget reached into the reserve: %s", c.name, opp.name, breach)
			}
		}
	}
}
