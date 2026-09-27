package main

import (
	"fmt"
	"time"

	"chess/engine"
	"chess/lichessbot"
)

// budgetRule is the lichessbot rule a -budget-rule name stands for: "old"
// is lichessbot.MoveTimeBudget, which reads only our own clock, and "new"
// is lichessbot.MoveBudget, which also reads the opponent's clock and how
// many moves we have played since the match opening. Both are the bot's
// own functions, not copies of them.
func budgetRule(name string) (engine.BudgetRule, error) {
	switch name {
	case "old":
		return func(v engine.ClockView) time.Duration {
			return lichessbot.MoveTimeBudget(v.OurMS, v.IncMS)
		}, nil
	case "new":
		return func(v engine.ClockView) time.Duration {
			return lichessbot.MoveBudget(lichessbot.Clock{
				OurMS: v.OurMS, OppMS: v.OppMS, IncMS: v.IncMS, OppIncMS: v.OppIncMS,
				MovesOutOfBook: v.MovesOutOfBook,
			})
		}, nil
	}
	return nil, fmt.Errorf("unknown budget rule %q: want old or new", name)
}

// clockMatch builds the match clock from -clock-ms, -inc-ms and
// -clock-scale, and puts each side on its budget rule. It returns nil and
// leaves both players alone when -clock-ms is 0.
func clockMatch(clockMS, incMS int, scale float64, rule, refRule string, challenger, reference *engine.Player) (*engine.MatchClock, error) {
	if clockMS <= 0 {
		return nil, nil
	}
	if scale <= 0 {
		return nil, fmt.Errorf("-clock-scale %g: must be above 0", scale)
	}
	if incMS < 0 {
		return nil, fmt.Errorf("-inc-ms %d: must not be negative", incMS)
	}
	cr, err := budgetRule(rule)
	if err != nil {
		return nil, fmt.Errorf("-budget-rule: %w", err)
	}
	rr, err := budgetRule(refRule)
	if err != nil {
		return nil, fmt.Errorf("-ref-budget-rule: %w", err)
	}
	challenger.ClockRule, reference.ClockRule = cr, rr
	return &engine.MatchClock{
		Initial:   time.Duration(clockMS) * time.Millisecond,
		Increment: time.Duration(incMS) * time.Millisecond,
		Scale:     scale,
	}, nil
}

// clockMatchNote says what the clock is and what game it stands for, in
// lichess's minutes+seconds.
func clockMatchNote(clockMS, incMS int, scale float64, rule, refRule string) string {
	minutes := float64(clockMS) * scale / 60000
	seconds := float64(incMS) * scale / 1000
	return fmt.Sprintf("both sides on a %d+%d ms clock at scale %g, a %g+%g game: challenger budgets by the %s rule, reference by the %s",
		clockMS, incMS, scale, minutes, seconds, rule, refRule)
}

// flagNote is the summary line for games lost on time.
func flagNote(res engine.MatchResult) string {
	return fmt.Sprintf("  lost on time: challenger %d, reference %d", res.FlagLosses, res.OppFlagLosses)
}
