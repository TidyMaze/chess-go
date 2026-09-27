package engine

import (
	"math"
	"time"

	"chess/board"
	"chess/game"
)

// ClockView is what a budget rule reads before one move: both clocks and
// both increments in milliseconds at the clock's scale, and how many moves
// the mover has already played since the match opening (0 for its first).
type ClockView struct {
	OurMS, OppMS, IncMS, OppIncMS int64
	MovesOutOfBook                int
}

// BudgetRule turns a clock into the budget for one move, at the clock's
// scale.
type BudgetRule func(ClockView) time.Duration

// MatchClock plays match games on a chess clock: each side starts with
// Initial, loses the wall time of every move it makes and then gains
// Increment, and loses the game when its clock reaches zero.
//
// Scale lets a short game stand for a long one. The rules see both clocks
// and the increment at Scale times their real value, and the budget they
// return is divided by Scale, so a 12+0.1 second game at scale 10 is
// budgeted as the 2+1 game it stands for, reserve rules included, while
// playing ten times faster. The clock itself runs in real time. 0 means 1.
type MatchClock struct {
	Initial, Increment time.Duration
	Scale              float64
	// pick plays a move and says how long it took. Nil times the player's
	// own search on the wall clock; a test puts a fake engine here.
	pick func(Player, *game.Game, *TranspositionTable) (game.Move, float64, bool, time.Duration)
}

// timedPick is the player's own move choice, timed on the wall clock.
func timedPick(p Player, g *game.Game, t *TranspositionTable) (game.Move, float64, bool, time.Duration) {
	start := time.Now()
	m, s, ok := p.pickScored(g, t)
	return m, s, ok, time.Since(start)
}

func (c *MatchClock) scale() float64 {
	if c.Scale <= 0 {
		return 1
	}
	return c.Scale
}

// scaledMS is a real duration as the rules see it, in milliseconds.
func (c *MatchClock) scaledMS(d time.Duration) int64 {
	return int64(math.Round(float64(d) / float64(time.Millisecond) * c.scale()))
}

// gameClock is one game's clocks, indexed by colour.
type gameClock struct {
	c     *MatchClock
	left  [2]time.Duration
	moves [2]int
}

func (c *MatchClock) start() *gameClock {
	return &gameClock{c: c, left: [2]time.Duration{c.Initial, c.Initial}}
}

// withBudget hands a player the budget for one move, on whichever of its
// two clocks it plays to. An external engine is never sent a movetime of
// zero, which it would read as no limit.
func withBudget(p Player, budget time.Duration) Player {
	p.TimeBudget = budget
	p.UCIMoveTimeMS = max(int(budget/time.Millisecond), 1)
	return p
}

// move plays side's move on the clock, and reports whether its clock ran
// out on it.
func (gc *gameClock) move(side board.Color, p Player, g *game.Game, t *TranspositionTable) (game.Move, float64, bool, bool) {
	if p.ClockRule != nil {
		v := ClockView{
			OurMS: gc.c.scaledMS(gc.left[side]), OppMS: gc.c.scaledMS(gc.left[side.Other()]),
			IncMS: gc.c.scaledMS(gc.c.Increment), OppIncMS: gc.c.scaledMS(gc.c.Increment),
			MovesOutOfBook: gc.moves[side],
		}
		p = withBudget(p, time.Duration(float64(p.ClockRule(v))/gc.c.scale()))
	}
	pick := gc.c.pick
	if pick == nil {
		pick = timedPick
	}
	m, score, ok, spent := pick(p, g, t)
	gc.left[side] -= spent
	if gc.left[side] <= 0 {
		return m, score, ok, true
	}
	gc.left[side] += gc.c.Increment
	gc.moves[side]++
	return m, score, ok, false
}

// gameOutcome is how one game ended; flag is set when the loser lost on
// time.
type gameOutcome struct {
	winner   board.Color
	decisive bool
	flag     bool
}

// PlayMatchOnClock is PlayMatch with every game played on clock c, each
// player budgeting its moves by its ClockRule.
func PlayMatchOnClock(a, b Player, games, maxMoves int, c *MatchClock) MatchResult {
	return playMatchClocked(a, b, games, maxMoves, nil, c)
}
