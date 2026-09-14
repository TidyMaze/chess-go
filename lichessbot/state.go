// Package lichessbot runs the champion engine as a lichess Bot API client:
// it accepts challenges, streams each game, and answers with the champion's
// own search. Nothing here scores a position with an outside engine; the
// move it sends is always PlayerPickWith on the champion, same as every
// other place this engine plays.
package lichessbot

import (
	"strings"
	"time"

	"chess/board"
	"chess/game"
)

// A challenge lichess offers the bot. Only the fields the decision needs.
type Challenge struct {
	ID       string
	Variant  string // "standard", "chess960", "atomic", ...
	Rated    bool
	SpeedTC  string // "bullet", "blitz", "rapid", "classical", "correspondence"
	FromBot  bool   // the challenger is itself a bot account
	Outgoing bool   // this bot sent the challenge; lichess echoes it back on the same stream
	Casual   bool
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
		g.ApplyMove(m.From, m.To)
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

// moveUCIForLichess is m.UCI() with the promotion letter lichess requires.
// The engine's own Move.UCI() omits it because ApplyMove always queens a
// pawn that reaches the last rank without being told which piece to
// become, which is fine between two calls into this engine but not
// understood as a promotion by the lichess API: a bare "e7e8" there is
// read as an illegal pawn move two ranks past the board, not a queening.
func moveUCIForLichess(g *game.Game, m game.Move) string {
	uci := m.UCI()
	p, ok := g.Board.PieceAt(m.From)
	if !ok || p.Type != board.Pawn {
		return uci
	}
	lastRank := 7 // rank 8, zero-indexed, for a white pawn
	if p.Color == board.Black {
		lastRank = 0 // rank 1
	}
	if m.To.Rank != lastRank {
		return uci
	}
	return uci + "q"
}

// unlimitedBudget is what one move gets in a game with no clock, a
// correspondence or unlimited challenge. Long enough to be a real search
// rather than a race budget, short enough that nobody waits on it.
const unlimitedBudget = 15 * time.Second

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
		// Past this a longer think buys about a ply per second, measured,
		// which is not worth the clock. See LEARNINGS.md.
		maxBudgetMs = 15000
	)
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
	budgetMs := float64(incMs)*incrementShare + spendable/spread - overhead.reserve()
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
	return time.Duration(budgetMs) * time.Millisecond
}

// MoveTimeBudget is moveTimeBudget for callers outside this package, so a
// tool can audit real games against the rule the bot actually plays by
// instead of against a copy of it. A copy is exactly how an earlier version
// of this rule was cleared: a hand written model of it said a 120 move game
// ended with 0.3 s in hand, and the real function flagged.
func MoveTimeBudget(remainingMS, incrementMS int64) time.Duration {
	return moveTimeBudget("white", gameState{WhiteTimeMS: remainingMS, WhiteIncMS: incrementMS}, nil)
}
