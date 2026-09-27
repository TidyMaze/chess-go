package lichessbot

import (
	"testing"
	"time"

	"chess/engine"
)

// The bot keeps the old budget rule: against it, the out-of-book boost and
// the opponent-clock compensation lost 16 +/- 14 Elo in scaled 2+1 games.
// What won (+58 +/- 15 together with them) is the stabletime stop, which may
// run past the budget up to a hard limit; the bot caps that limit at an
// eighth of its clock so a long think cannot eat the reserve.
func TestEffectivePlayerPlaysTheOldRuleWithAHardCap(t *testing.T) {
	base := engine.Player{TimeBudget: time.Second}
	overhead := newOverheadEstimate()
	for _, st := range []gameState{
		{WhiteTimeMS: 120000, WhiteIncMS: 1000, BlackTimeMS: 15000, BlackIncMS: 1000},
		{WhiteTimeMS: 10000, WhiteIncMS: 1000, BlackTimeMS: 90000, BlackIncMS: 1000},
		{WhiteTimeMS: 3000, BlackTimeMS: 3000},
	} {
		got := effectivePlayer(base, "white", st, "bullet", overhead, 0)
		want := moveTimeBudget("white", st, overhead)
		if got.TimeBudget != want {
			t.Errorf("clock %d ms: budget %v, want the old rule's %v", st.WhiteTimeMS, got.TimeBudget, want)
		}
		wantHard := 2 * want
		if eighth := time.Duration(st.WhiteTimeMS) * time.Millisecond / 8; wantHard > eighth {
			wantHard = eighth
		}
		if wantHard < want {
			wantHard = want
		}
		if got.HardBudget != wantHard {
			t.Errorf("clock %d ms: hard limit %v, want %v", st.WhiteTimeMS, got.HardBudget, wantHard)
		}
	}
}
