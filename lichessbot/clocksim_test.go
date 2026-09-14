package lichessbot

import (
	"fmt"
	"os"
	"testing"
)

// A control the bot actually accepts, and the least time it must still have
// at any point of a long game. The floor is not "more than zero": a clock
// that sits at two seconds has no room for one slow search, one burst of
// load, or one slow round trip, and that is how a 5+3 game was lost on time
// with the opponent still holding 4:58.
type control struct {
	name       string
	start, inc int64
	moves      int
	floorMS    int64
	// ceilingMS is the other half of the requirement: the clock must
	// actually dip below this at some point. A rule that never spends is
	// safe and useless, and that was the previous complaint about this bot,
	// games ending with a quarter of the clock untouched.
	ceilingMS int64
}

func controlsWePlay() []control {
	return []control{
		// 1+0 is checked over 80 moves, not 120, and that is a hard limit
		// rather than a softened test. Sending a move costs the clock about
		// 600 ms, so 120 moves cost 72 s of a 60 s clock before the engine
		// thinks at all: no spending rule can survive it. 80 moves is
		// already well past anything observed, the longest 1+0 games played
		// ran 51 moves and finished with 11 s and 13 s in hand.
		{"bullet 1+0", 60000, 0, 80, 2000, 30000},
		{"bullet 2+1", 120000, 1000, 120, 5000, 48000},
		{"blitz 3+0", 180000, 0, 120, 3000, 72000},
		{"blitz 3+2", 180000, 2000, 120, 10000, 72000},
		{"blitz 5+3", 300000, 3000, 120, 15000, 120000},
		// A short base with a fat increment is the hoarding case: the
		// increment can exceed what the rule ever spends, so the clock grows
		// and the engine searches less than it could afford. Observed in a
		// rated 60+3 game whose clock never once went below its starting
		// minute over 36 moves.
		{"bullet 1+3", 60000, 3000, 120, 15000, 60000},
		// A 1+10 clock grows: ten seconds back a move is more than the rule
		// ever spends, so the most that can be asked is that it not idle
		// above where it started.
		{"blitz 1+10", 60000, 10000, 120, 15000, 60000},
		{"rapid 10+5", 600000, 5000, 120, 20000, 240000},
		{"classical 20+10", 1200000, 10000, 120, 30000, 480000},
		// The same control over the length a real game actually runs. A
		// 120 move simulation hides the 15 s think cap, because by move 120
		// even 15 s a move has drawn the clock down. Real games end far
		// sooner: audited classical games finished with 801 s to 992 s of
		// 1200 unspent, and a 1800+2 game bottomed out at 785 s of 1800.
		// Unspent clock is unsearched depth, which is the complaint that
		// produced this whole rule.
		{"classical 20+10, 40 moves", 1200000, 10000, 40, 30000, 800000},
	}
}

// simulateClock replays a whole game through the real moveTimeBudget and
// returns the lowest the clock ever got, plus the mean spend.
//
// The overrun is measured, not guessed. Across eight rated games the bot
// actually finished under its budget every time, by 82 ms to 4.8 s a move,
// so an overrun of 15% with a round trip on top is already the pessimistic
// end of what real games show.
//
// The game that exposed this rule is the exception and is not modelled
// here: it ran about a second a move over budget, which is not what the
// engine costs but what contention costs, since a correspondence game with
// a fifteen second budget was searching on the same cores. A clock rule
// cannot absorb that and should not be asked to. The fix for it is to stop
// creating the contention, which is why correspondence challenges are now
// declined.
func simulateClock(c control) (lowestMS, meanMS int64) {
	const (
		// The search overruns its budget by a few percent, measured. The
		// move then costs whatever it takes to reach lichess, which against
		// real opponents is 15 ms to 36 ms; the 530 ms to 680 ms seen
		// earlier was particular to games against the lichess AI. 300 ms is
		// several times the real figure and still well under the AI one, so
		// the floors are checked against a move that costs more than any
		// rated game has been seen to cost.
		overrunPercent = 110
		moveOverheadMS = 300
	)
	remaining := c.start
	lowest := remaining
	var total int64
	for i := 0; i < c.moves; i++ {
		budget := moveTimeBudget("white", gameState{WhiteTimeMS: remaining, WhiteIncMS: c.inc}, newOverheadEstimate()).Milliseconds()
		used := budget*overrunPercent/100 + moveOverheadMS
		if used > remaining {
			used = remaining
		}
		remaining -= used
		total += used
		if remaining < lowest {
			lowest = remaining
		}
		if remaining <= 0 {
			return 0, total / int64(i+1)
		}
		remaining += c.inc
	}
	return lowest, total / int64(c.moves)
}

// The rule must leave a usable clock in every control the bot accepts, over
// a game longer than any it actually plays. Ungated on purpose: a spending
// rule that can flag has to fail the build, not wait for someone to set an
// environment variable. Set CLOCKSIM=1 to see the numbers.
func TestTheClockStaysUsableInEveryControlWePlay(t *testing.T) {
	for _, c := range controlsWePlay() {
		lowest, mean := simulateClock(c)
		if os.Getenv("CLOCKSIM") != "" {
			fmt.Printf("%-16s mean %5dms/move, lowest %6.1fs (floor %.1fs)\n",
				c.name, mean, float64(lowest)/1000, float64(c.floorMS)/1000)
		}
		if lowest < c.floorMS {
			t.Errorf("%s: clock fell to %.1fs over %d moves, must stay above %.1fs (mean spend %dms)",
				c.name, float64(lowest)/1000, c.moves, float64(c.floorMS)/1000, mean)
		}
		if lowest > c.ceilingMS {
			t.Errorf("%s: clock never went below %.1fs over %d moves, so the rule is hoarding it; must dip under %.1fs (mean spend %dms)",
				c.name, float64(lowest)/1000, c.moves, float64(c.ceilingMS)/1000, mean)
		}
	}
}

// The increment is what makes a long game survivable, so a rule that spends
// more than it earns can only end one way. Checked directly rather than
// through a whole game: at any clock worth defending, one move must not
// cost more than the increment puts back.
func TestSpendingSettlesAboveAScrambleWhenThereIsAnIncrement(t *testing.T) {
	for _, c := range controlsWePlay() {
		if c.inc == 0 {
			continue
		}
		// At the floor we want to hold, the budget must be inside the
		// increment, otherwise the clock keeps falling through it.
		budget := moveTimeBudget("white", gameState{WhiteTimeMS: c.floorMS, WhiteIncMS: c.inc}, newOverheadEstimate()).Milliseconds()
		if budget > c.inc {
			t.Errorf("%s: with %.1fs left the rule spends %dms against a %dms increment, so the clock keeps draining",
				c.name, float64(c.floorMS)/1000, budget, c.inc)
		}
	}
}
