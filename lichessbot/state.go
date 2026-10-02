// Package lichessbot runs the champion engine as a lichess Bot API client:
// it accepts challenges, streams each game, and answers with the champion's
// own search. Nothing here scores a position with an outside engine; the
// move it sends is always PlayerPickWith on the champion, same as every
// other place this engine plays.
package lichessbot

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"chess/board"
	"chess/game"
)

// A challenge lichess offers the bot. Only the fields the decision needs.
type Challenge struct {
	ID         string
	Variant    string // "standard", "chess960", "atomic", ...
	Rated      bool
	SpeedTC    string // "bullet", "blitz", "rapid", "classical", "correspondence"
	FromBot    bool   // the challenger is itself a bot account
	Outgoing   bool   // this bot sent the challenge; lichess echoes it back on the same stream
	Casual     bool
	Challenger string
}

// shouldAcceptChallenge decides whether to accept, without touching the
// network so the policy can be tested on its own.
//
// Standard chess only: the champion has never played a variant and a
// variant game would silently exercise nothing this engine was built for.
// Anything faster than bullet's floor is declined too: at lichess bullet
// (3+0) the champion gets at most a few seconds a move against clients
// that expect sub-second replies, and a flagged loss on time measures the
// network round trip, not the engine.
func shouldAcceptChallenge(c Challenge) bool {
	if c.Variant != "" && c.Variant != "standard" {
		return false
	}
	if c.SpeedTC == "ultraBullet" {
		return false
	}
	// Correspondence is declined, and the reason is the clock rather than
	// the chess. It moves none of the four ratings that are being chased,
	// it holds one of the MaxGames slots for days, and a move in it is
	// searched for fifteen seconds on the same cores as every real time
	// game. That last part is not theoretical: blitz game hTmspQs0 was lost
	// on time while a correspondence game searched beside it, spending
	// about a second a move more than its budget, which is contention, not
	// engine cost. Every other measured game finished under budget.
	if c.SpeedTC == "correspondence" {
		return false
	}
	return true
}

// applyMovesString replays a lichess move list (space-separated UCI moves,
// as gameState.moves arrives) onto a fresh game from the given FEN, or the
// start position when fen is "startpos" or empty. An unparseable move stops
// the replay there rather than applying moves out of order; the caller sees
// the position as it stood before the bad move.
func applyMovesString(fen, moves string) (*game.Game, error) {
	var g *game.Game
	var err error
	if fen == "" || fen == "startpos" {
		g = game.New()
	} else {
		g, err = game.ParseFEN(fen)
		if err != nil {
			return nil, err
		}
		// ParseFEN leaves repetition tracking off, which is right for the
		// search's own throwaway positions and wrong for a game being
		// played: the move picker's only anti-shuffle rule is to prefer a
		// move that does not return to a position it has already stood in,
		// and that rule is dead without tracking. game.New already turns it
		// on, so only this branch was missing it.
		g.EnableRepetitionTracking()
	}
	if strings.TrimSpace(moves) == "" {
		return g, nil
	}
	for _, s := range strings.Fields(moves) {
		m, ok := game.MoveFromUCI(s)
		if !ok {
			break
		}
		g.Apply(m)
	}
	return g, nil
}

// isOurTurn compares the game's side to move against the color the bot is
// playing in this game, both derived from data the caller already parsed
// out of the gameFull/gameState events.
func isOurTurn(g *game.Game, ourColor string) bool {
	if ourColor == "white" {
		return g.Turn == board.White
	}
	return g.Turn == board.Black
}

// unlimitedBudget is what one move gets in a game with no clock, a
// correspondence or unlimited challenge. Long enough to be a real search
// rather than a race budget, short enough that nobody waits on it.
const unlimitedBudget = 15 * time.Second

// minMaxBudgetMs is a floor under the per-move ceiling, not the ceiling
// itself. Past this a longer think buys little on a short clock, but on a
// long one the flat cap was the binding constraint rather than the spending
// rule: audited classical games finished with 801 s to 992 s of 1200
// unspent, and a 1800+2 game bottomed out at 785 s of 1800, because every
// think was cut to fifteen seconds however much clock was sitting there.
// Unspent clock is unsearched depth, which is the complaint this whole rule
// exists to answer.
//
// maxBudgetDivisor scales the ceiling with what is actually on the clock. A
// fiftieth is deliberately conservative: it only lifts the cap once there is
// more than 750 s left, so bullet, blitz and rapid never reach it and
// nothing about them changes.
const (
	minMaxBudgetMs   = 15000
	maxBudgetDivisor = 50
)

// moveTimeBudget turns the live clock lichess sends into a per-move
// thinking budget, so the engine spends more time in a long game and less
// in a short one instead of always thinking for whatever champion.json
// says.
//
// The shape that matters is what the rule settles at, not what it spends on
// move one. Spending a share of the clock plus a share of the increment
// converges on a fixed point, and that fixed point is the whole game: put
// it too low and every long game is played in a scramble. This one keeps a
// reserve it never touches, spreads what is above it over the moves still
// to come, and takes only half the increment, which settles around 45 s in
// a 5+3 game instead of around 2.
//
// Whole games at every control the bot accepts are simulated against this
// function in clocksim_test.go, with the overrun and round trip a real game
// pays, and the clock has to stay above a floor in all of them.
//
// The bot now plays by moveBudget, which boosts this base for the first
// moves out of book and scales it by the opponent's clock, then applies the
// same caps. This function stays exactly as it was, because clockaudit
// scores games with it through MoveTimeBudget.
//
// overheadEstimate tracks what a move costs this game beyond its search,
// because a single constant cannot serve both kinds of opponent. Measured
// on 2026-09-14: posting a move takes 15 ms to 36 ms against a real bot and
// 543 ms to 736 ms against the lichess AI, in the same build, minutes
// apart. Reserving the small figure loses a game against the AI; reserving
// the large one throws away a third of the thinking time in every rated
// bullet game.
//
// So it is measured rather than assumed. The estimate rises quickly and
// falls slowly: an overhead that turns out to be bigger than expected costs
// clock immediately, while one that turns out smaller only costs a little
// unused depth, so the two errors are not worth the same.
type overheadEstimate struct {
	ms float64
}

const (
	defaultOverheadMs = 150.0
	maxOverheadMs     = 1500.0
)

func newOverheadEstimate() *overheadEstimate {
	return &overheadEstimate{ms: defaultOverheadMs}
}

// observe folds one measured post into the estimate.
func (o *overheadEstimate) observe(d time.Duration) {
	ms := float64(d.Milliseconds())
	if ms < 0 {
		return
	}
	const (
		riseWeight = 0.6 // a surprise upward is taken seriously at once
		fallWeight = 0.2 // a surprise downward is taken slowly
	)
	w := fallWeight
	if ms > o.ms {
		w = riseWeight
	}
	o.ms = (1-w)*o.ms + w*ms
	if o.ms < defaultOverheadMs {
		o.ms = defaultOverheadMs
	}
	if o.ms > maxOverheadMs {
		o.ms = maxOverheadMs
	}
}

func (o *overheadEstimate) reserve() float64 {
	if o == nil {
		return defaultOverheadMs
	}
	return o.ms
}

const (
	// Only half the increment is spent, not most of it. The rule this
	// replaced spent 0.9 of it plus a twentieth of the clock, which
	// solves to an equilibrium near 0.7 of the increment: every game
	// long enough drifted into a permanent two second scramble and
	// stayed there. Game hTmspQs0 was lost exactly that way, flagging a
	// 5+3 blitz game while the opponent still held 4:58. Half leaves
	// enough margin that the clock settles well above the reserve
	// instead of falling through it.
	incrementShare = 0.5
	// What is left above the reserve is spread over this many moves, so
	// spending tracks the clock rather than a fixed guess at the move.
	movesToGo = 30
	// Without an increment there is no equilibrium to settle at: the
	// clock only goes down, and every move also costs a round trip
	// whatever the search does. So the same clock is spread much
	// thinner, which is the one thing the rule this replaced got right.
	movesToGoWithoutIncrement = 80
	// The reserve is never spent. It grows with the increment, because
	// a bigger increment means bigger thinks and more to lose to one
	// slow search, and is capped as a share of the clock so a 1+10 game
	// does not reserve most of what it has.
	baseReserveMs     = 2000
	reserveIncrements = 5
	maxReserveShare   = 0.5

	// What a move costs beyond its search is no longer a constant: it
	// is measured per game by overheadEstimate above, because it is 15 ms
	// against a real opponent and 700 ms against the lichess AI.

	safetyMarginMs = 200
	minBudgetMs    = 50

	// No position needs more than 30s on local hardware: at depth 24 the engine
	// is already searching the universe. A 1800s classical clock gives 36s via
	// the share rule, which makes opponents disconnect (game blbndbPY).
	absMaxBudgetMs = 30000
)

func moveTimeBudget(ourColor string, st gameState, overhead *overheadEstimate) time.Duration {
	remainMs, incMs := st.WhiteTimeMS, st.WhiteIncMS
	if ourColor == "black" {
		remainMs, incMs = st.BlackTimeMS, st.BlackIncMS
	}
	if remainMs <= 0 {
		// No clock reported. Let the caller decide: a correspondence game
		// gets a real budget, anything else falls back to the champion's.
		return 0
	}
	budgetMs := capBudgetMs(baseBudgetMs(remainMs, incMs, overhead), remainMs)
	return time.Duration(budgetMs) * time.Millisecond
}

// baseBudgetMs is the spending rule before any cap: half the increment,
// plus what is above the reserve spread over the moves still to come,
// less what posting the move is expected to cost.
func baseBudgetMs(remainMs, incMs int64, overhead *overheadEstimate) float64 {
	reserveMs := float64(baseReserveMs + reserveIncrements*incMs)
	if maxReserve := float64(remainMs) * maxReserveShare; reserveMs > maxReserve {
		reserveMs = maxReserve
	}
	// Never negative: the reserve is capped at half the clock just above.
	spendable := float64(remainMs) - reserveMs
	spread := float64(movesToGo)
	if incMs <= 0 {
		spread = movesToGoWithoutIncrement
	}
	return float64(incMs)*incrementShare + spendable/spread - overhead.reserve()
}

// capBudgetMs holds a budget to the safety rules, in this order: the
// ceiling that scales with the clock, the floor that still lets a move be
// played, the margin under what is left, and the absolute cap.
func capBudgetMs(budgetMs float64, remainMs int64) float64 {
	maxBudgetMs := float64(remainMs) / maxBudgetDivisor
	if maxBudgetMs < minMaxBudgetMs {
		maxBudgetMs = minMaxBudgetMs
	}
	if budgetMs > maxBudgetMs {
		budgetMs = maxBudgetMs
	}
	if budgetMs < minBudgetMs {
		budgetMs = minBudgetMs
	}
	ceiling := float64(remainMs) - safetyMarginMs
	if ceiling < minBudgetMs {
		// Almost out of time: think as little as the floor allows rather
		// than refuse to move.
		ceiling = minBudgetMs
	}
	if budgetMs > ceiling {
		budgetMs = ceiling
	}
	if budgetMs > absMaxBudgetMs {
		budgetMs = absMaxBudgetMs
	}
	return budgetMs
}

// MoveTimeBudget is moveTimeBudget for callers outside this package, so a
// tool can audit real games against the rule itself instead of against a
// copy of it. It is the base of the rule the bot plays by, without the out
// of book boost and the opponent clock factor that MoveBudget adds. A copy
// is exactly how an earlier version
// of this rule was cleared: a hand written model of it said a 120 move game
// ended with 0.3 s in hand, and the real function flagged.
func MoveTimeBudget(remainingMS, incrementMS int64) time.Duration {
	return moveTimeBudget("white", gameState{WhiteTimeMS: remainingMS, WhiteIncMS: incrementMS}, nil)
}

// Clock is what the rule the bot plays by reads for one move: both clocks
// and both increments in milliseconds, and how many of our moves have been
// played since the book first failed to supply one (0 for that first move).
type Clock struct {
	OurMS, OppMS, IncMS, OppIncMS int64
	MovesOutOfBook                int
}

const (
	// The first moves after the book are where the game is decided while
	// the clock is still full, so the first gets 1.8x the base and the
	// extra falls by a tenth of 0.8 a move, to nothing from the tenth on.
	outOfBookMoves = 10
	outOfBookExtra = 0.8
	// How far the opponent's clock may move the budget either way.
	minCompensation = 0.7
	maxCompensation = 1.4
	// Whatever the boosts ask for, one move never takes more than an
	// eighth of what is left. Since the reserve is at most half the clock,
	// this also keeps every budget out of the reserve.
	maxClockShareDivisor = 8
)

// outOfBookBoost is the multiplier for the k-th of our moves out of book.
func outOfBookBoost(k int) float64 {
	if k < 0 || k >= outOfBookMoves {
		return 1
	}
	return 1 + outOfBookExtra*float64(outOfBookMoves-k)/outOfBookMoves
}

// clockCompensation spends more when the opponent is short of time and
// less when we are: the square root of our clock over theirs, clamped. An
// opponent clock of zero counts as one millisecond, so the ratio stays
// finite and the clamp decides.
func clockCompensation(ourMS, oppMS int64) float64 {
	r := float64(ourMS) / float64(max(oppMS, 1))
	return min(max(math.Sqrt(r), minCompensation), maxCompensation)
}

// moveBudget is the rule the bot plays by. It takes moveTimeBudget's base,
// multiplies it by the out of book boost and the opponent clock factor,
// and only then applies every cap moveTimeBudget has, plus the eighth of
// the clock. The boosts can move time between moves; they cannot touch the
// margins that keep a slow search from flagging.
//
// It keeps microseconds rather than whole milliseconds, so that below 8 ms
// an eighth of the clock is still a positive budget: zero means "no clock"
// to the caller, which then falls back to the champion's own budget.
func moveBudget(c Clock, overhead *overheadEstimate) time.Duration {
	if c.OurMS <= 0 {
		return 0
	}
	budgetMs := baseBudgetMs(c.OurMS, c.IncMS, overhead) *
		outOfBookBoost(c.MovesOutOfBook) * clockCompensation(c.OurMS, c.OppMS)
	budgetMs = capBudgetMs(budgetMs, c.OurMS)
	budgetMs = min(budgetMs, float64(c.OurMS)/maxClockShareDivisor)
	return time.Duration(math.Round(budgetMs*1000)) * time.Microsecond
}

// MoveBudget is moveBudget for callers outside this package, with the
// default overhead estimate, as MoveTimeBudget has.
func MoveBudget(c Clock) time.Duration { return moveBudget(c, nil) }

// DynamicBudgetParams configures the dynamic time allocation rule.
type DynamicBudgetParams struct {
	MidgameBoost   float64
	OpeningFactor  float64
	MidgameEndMove int
	ClockCompExp   float64
	ClockCompMax   float64
	ClockShareDiv  float64
}

// DefaultDynamicParams is the baseline calibrated configuration.
var DefaultDynamicParams = DynamicBudgetParams{
	MidgameBoost:   1.10,
	OpeningFactor:  0.85,
	MidgameEndMove: 32,
	ClockCompExp:   0.60,
	ClockCompMax:   2.00,
	ClockShareDiv:  7.0,
}

// ActiveDynamicParams is the active configuration used in games.
var ActiveDynamicParams = DefaultDynamicParams

// ParseDynamicParams parses comma-separated key=value pairs into a DynamicBudgetParams.
func ParseDynamicParams(spec string) (DynamicBudgetParams, error) {
	p := DefaultDynamicParams
	if spec == "" {
		return p, nil
	}
	parts := strings.Split(spec, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return p, fmt.Errorf("invalid param %q: want key=value", part)
		}
		val, err := strconv.ParseFloat(kv[1], 64)
		if err != nil {
			return p, fmt.Errorf("param %q: invalid number %q", kv[0], kv[1])
		}
		switch kv[0] {
		case "mid":
			p.MidgameBoost = val
		case "open":
			p.OpeningFactor = val
		case "end":
			p.MidgameEndMove = int(val)
		case "exp":
			p.ClockCompExp = val
		case "max":
			p.ClockCompMax = val
		case "share":
			p.ClockShareDiv = val
		default:
			return p, fmt.Errorf("unknown param key %q", kv[0])
		}
	}
	return p, nil
}

// moveBudgetDynamicWithParams dynamically balances time usage with custom parameters.
func moveBudgetDynamicWithParams(c Clock, overhead *overheadEstimate, p DynamicBudgetParams) time.Duration {
	if c.OurMS <= 0 {
		return 0
	}
	move := c.MovesOutOfBook

	// 1. Dynamic remaining moves M
	var m float64
	if c.IncMS > 0 {
		m = math.Max(18, 40.0-0.4*float64(move))
	} else {
		m = math.Max(20, 65.0-0.5*float64(move))
	}

	// 2. Base budget with reserve
	reserveMs := float64(baseReserveMs + reserveIncrements*c.IncMS)
	if maxReserve := float64(c.OurMS) * maxReserveShare; reserveMs > maxReserve {
		reserveMs = maxReserve
	}
	spendable := math.Max(0, float64(c.OurMS)-reserveMs)
	base := spendable/m + float64(c.IncMS)*incrementShare
	if overhead != nil {
		base -= overhead.reserve()
	}

	// 3. Dynamic game phase factor
	var phaseBoost float64
	if move < 8 {
		phaseBoost = p.OpeningFactor
	} else if move <= p.MidgameEndMove {
		phaseBoost = p.MidgameBoost
	} else {
		phaseBoost = 1.0
	}

	// 4. Clock compensation relative to opponent
	r := float64(c.OurMS) / float64(max(c.OppMS, 1))
	var kClock float64
	if r > 1.25 {
		kClock = math.Min(p.ClockCompMax, math.Pow(r/1.15, p.ClockCompExp))
	} else if r < 1.05 {
		kClock = math.Max(0.45, math.Pow(r, 1.2))
	} else {
		kClock = 1.0
	}

	budgetMs := base * phaseBoost * kClock

	// 5. Dynamic ceiling and safety caps
	estTotalClock := float64(max(c.OurMS, c.OppMS))
	dynamicMax := math.Min(60000, math.Max(float64(minMaxBudgetMs), 0.05*estTotalClock+2*float64(c.IncMS)))
	budgetMs = math.Min(budgetMs, dynamicMax)
	if p.ClockShareDiv > 0 {
		budgetMs = math.Min(budgetMs, float64(c.OurMS)/p.ClockShareDiv)
	}

	minB := math.Max(float64(minBudgetMs), float64(c.OurMS)*0.002)
	ceiling := float64(c.OurMS) - float64(safetyMarginMs)
	if ceiling < minB {
		ceiling = minB
	}
	budgetMs = math.Min(budgetMs, ceiling)
	budgetMs = math.Max(budgetMs, minB)

	return time.Duration(math.Round(budgetMs*1000)) * time.Microsecond
}

// moveBudgetDynamic dynamically balances time usage across all time controls:
// it budgets based on game phase, expected remaining moves, clock ratio vs opponent,
// and proportional safety margins.
func moveBudgetDynamic(c Clock, overhead *overheadEstimate) time.Duration {
	return moveBudgetDynamicWithParams(c, overhead, ActiveDynamicParams)
}

// MoveBudgetDynamic is moveBudgetDynamic with default overhead estimate.
func MoveBudgetDynamic(c Clock) time.Duration { return moveBudgetDynamic(c, nil) }

// MoveBudgetDynamicWithParams is moveBudgetDynamic with custom parameters.
func MoveBudgetDynamicWithParams(c Clock, p DynamicBudgetParams) time.Duration {
	return moveBudgetDynamicWithParams(c, nil, p)
}


// clockFor reads our clock and the opponent's out of a game state.
func clockFor(ourColor string, st gameState, movesOutOfBook int) Clock {
	c := Clock{
		OurMS: st.WhiteTimeMS, OppMS: st.BlackTimeMS,
		IncMS: st.WhiteIncMS, OppIncMS: st.BlackIncMS,
		MovesOutOfBook: movesOutOfBook,
	}
	if ourColor == "black" {
		c.OurMS, c.OppMS = c.OppMS, c.OurMS
		c.IncMS, c.OppIncMS = c.OppIncMS, c.IncMS
	}
	return c
}
