package lichessbot

import (
	"fmt"
	"os"
	"testing"
)

// Replays a whole game's clock through the real moveTimeBudget, so the
// spending rule is checked as shipped rather than against a copy of it.
// A copy is exactly how the first version of this fix passed: a mock of
// the rule said a 120 move game at 1+0 ended with 0.3 s in hand, and the
// real function flagged.
//
// CLOCKSIM=1 to print it.
func TestClockSimulation(t *testing.T) {
	if os.Getenv("CLOCKSIM") == "" {
		t.Skip("CLOCKSIM=1 prints whole-game clock usage")
	}
	for _, c := range []struct {
		name       string
		start, inc int64
		moves      int
	}{
		{"bullet 2+1  40mv", 120000, 1000, 40},
		{"bullet 2+1  80mv", 120000, 1000, 80},
		{"bullet 1+0  80mv", 60000, 0, 80},
		{"bullet 1+0 120mv", 60000, 0, 120},
		{"blitz 3+2  100mv", 180000, 2000, 100},
		{"blitz 5+3   40mv", 300000, 3000, 40},
		{"blitz 5+3   80mv", 300000, 3000, 80},
		{"blitz 3+0   60mv", 180000, 0, 60},
		{"blitz 1+10  60mv", 60000, 10000, 60},
	} {
		r := c.start
		var total int64
		flagged := false
		for i := 0; i < c.moves; i++ {
			b := moveTimeBudget("white", gameState{WhiteTimeMS: r, WhiteIncMS: c.inc}).Milliseconds()
			// The search overruns its budget by about 7%, measured.
			used := b * 107 / 100
			if used > r {
				used = r
			}
			r -= used
			r += c.inc
			total += used
			if r <= 0 {
				flagged = true
				break
			}
		}
		note := ""
		if flagged {
			note = "  <-- FLAGGED"
		}
		fmt.Printf("%-18s mean %5dms/move, %6.1fs left%s\n",
			c.name, total/int64(c.moves), float64(r)/1000, note)
		if flagged {
			t.Errorf("%s flagged: the rule must never spend the whole clock", c.name)
		}
	}
}
