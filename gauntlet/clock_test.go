package main

import (
	"testing"
	"time"

	"chess/engine"
	"chess/lichessbot"
)

// A clock where the two lichessbot rules disagree: the opponent is short
// of time and this is our first move out of book, both of which the new
// rule spends on and the old one ignores.
var ruleProbe = engine.ClockView{OurMS: 180000, OppMS: 30000, IncMS: 2000, OppIncMS: 2000}

func oldRule(v engine.ClockView) time.Duration {
	return lichessbot.MoveTimeBudget(v.OurMS, v.IncMS)
}

func newRule(v engine.ClockView) time.Duration {
	return lichessbot.MoveBudget(lichessbot.Clock{
		OurMS: v.OurMS, OppMS: v.OppMS, IncMS: v.IncMS, OppIncMS: v.OppIncMS, MovesOutOfBook: v.MovesOutOfBook,
	})
}

// "old" is the bot's previous rule, blind to the opponent; "new" is the
// one it plays by now, and it reads every field of the clock.
func TestBudgetRuleNamesPickTheirFunction(t *testing.T) {
	if oldRule(ruleProbe) == newRule(ruleProbe) {
		t.Fatalf("the probe clock does not tell the rules apart: both give %s", oldRule(ruleProbe))
	}
	later := ruleProbe
	later.MovesOutOfBook, later.OppMS = 12, 400000
	for _, tc := range []struct {
		name string
		want func(engine.ClockView) time.Duration
	}{{"old", oldRule}, {"new", newRule}} {
		rule, err := budgetRule(tc.name)
		if err != nil {
			t.Fatalf("budgetRule(%q): %v", tc.name, err)
		}
		for _, v := range []engine.ClockView{ruleProbe, later} {
			if got, want := rule(v), tc.want(v); got != want {
				t.Errorf("rule %q on %+v = %s, want %s", tc.name, v, got, want)
			}
		}
	}
	if _, err := budgetRule("fast"); err == nil {
		t.Error("an unknown rule name was accepted")
	}
}

// -clock-ms 0 leaves the match off the clock and both players as they
// were; otherwise every flag reaches the clock, and each side gets its own
// rule.
func TestClockFlagsSetTheClockAndEachSidesRule(t *testing.T) {
	var challenger, reference engine.Player
	c, err := clockMatch(12000, 100, 10, "new", "old", &challenger, &reference)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.Initial != 12*time.Second || c.Increment != 100*time.Millisecond || c.Scale != 10 {
		t.Fatalf("clockMatch(12000, 100, 10) = %+v, want 12s + 100ms at scale 10", c)
	}
	if challenger.ClockRule == nil || reference.ClockRule == nil {
		t.Fatal("a side was left without a budget rule")
	}
	if got, want := challenger.ClockRule(ruleProbe), newRule(ruleProbe); got != want {
		t.Errorf("challenger on -budget-rule new budgets %s, want %s", got, want)
	}
	if got, want := reference.ClockRule(ruleProbe), oldRule(ruleProbe); got != want {
		t.Errorf("reference on -ref-budget-rule old budgets %s, want %s", got, want)
	}

	var p, q engine.Player
	if c, err := clockMatch(0, 0, 1, "new", "new", &p, &q); c != nil || err != nil || p.ClockRule != nil || q.ClockRule != nil {
		t.Errorf("-clock-ms 0 gave clock %+v, err %v, rules set %v %v; want no clock and no rules",
			c, err, p.ClockRule != nil, q.ClockRule != nil)
	}

	for _, bad := range []struct {
		name          string
		clock, inc    int
		scale         float64
		rule, refRule string
	}{
		{"zero scale", 12000, 100, 0, "old", "old"},
		{"negative increment", 12000, -1, 1, "old", "old"},
		{"unknown rule", 12000, 100, 1, "fast", "old"},
		{"unknown reference rule", 12000, 100, 1, "old", "fast"},
	} {
		var p, q engine.Player
		if _, err := clockMatch(bad.clock, bad.inc, bad.scale, bad.rule, bad.refRule, &p, &q); err == nil {
			t.Errorf("%s was accepted", bad.name)
		}
	}
}

// The summary names the side that lost on time, and says nothing off the
// clock.
func TestTheSummaryReportsFlagLosses(t *testing.T) {
	res := engine.MatchResult{Wins: 3, Losses: 2, FlagLosses: 2, OppFlagLosses: 1}
	if got, want := flagNote(res), "  lost on time: challenger 2, reference 1"; got != want {
		t.Errorf("flagNote = %q, want %q", got, want)
	}
}

// The note says what game the scaled clock stands for, as lichess writes
// a time control: minutes + seconds.
func TestTheClockNoteNamesTheGameItStandsFor(t *testing.T) {
	got := clockMatchNote(12000, 100, 10, "new", "old")
	want := "both sides on a 12000+100 ms clock at scale 10, a 2+1 game: challenger budgets by the new rule, reference by the old"
	if got != want {
		t.Errorf("clockMatchNote = %q, want %q", got, want)
	}
}
