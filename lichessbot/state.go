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
// says. Both remaining time and increment matter: a move played on
// increment alone should not eat into the clock, and a move played with
// little time left must not overrun it.
//
// The formula: a fixed fraction of what is left, plus most of the
// increment, so the clock is spent roughly evenly across the rest of the
// game rather than greedily up front. The result never exceeds what is
// actually left, minus a safety margin so a slow move never times out the
// game, and it is capped above so a very long time control does not make
// one move think forever for no measured gain (10.6 plies in 1s already
// only gains about a ply per second beyond that, see LEARNINGS.md).
func moveTimeBudget(ourColor string, st gameState) time.Duration {
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
		fractionOfRemaining      = 1.0 / 20.0
		fractionWithoutIncrement = 1.0 / 30.0
		fractionOfIncrement      = 0.9
		safetyMarginMs           = 200
		minBudgetMs              = 50
		maxBudgetMs              = 15000
		// The reserve is what a long game runs on. Spending is generous
		// above it and throttles hard below, so a game that goes long
		// slows down by itself instead of flagging.
		baseReserveMs     = 8000
		reserveIncrements = 10
		maxReserveShare   = 0.25
	)
	// A sixth to a quarter of the clock was going unspent in real games:
	// the thirteen lost bullet and blitz games ended with between 10.4 s
	// of a 60 s clock and 84.5 s of a 180 s one still on it, and none was
	// ever close to flagging. Unspent clock is unsearched depth, and
	// depth is what those losses were short of. Simulated over a whole
	// game, a flat smaller divisor buys that back but reaches zero in a
	// 120 move game without increment; holding a reserve buys most of it
	// and does not.
	// Increment is what makes generous spending safe: every move hands
	// some of it back, so a long game cannot drain the clock the way it
	// can without one. Simulated over 120 moves of 1+0, spending a
	// twentieth of what is left exhausts the clock exactly and flags,
	// while a thirtieth ends with half a second in hand. So the share
	// depends on whether there is an increment to lean on.
	share := fractionOfRemaining
	if incMs <= 0 {
		share = fractionWithoutIncrement
	}
	budgetMs := float64(remainMs)*share + float64(incMs)*fractionOfIncrement
	if budgetMs > maxBudgetMs {
		budgetMs = maxBudgetMs
	}
	// The reserve is capped as a share of the clock: ten increments is
	// the right idea on a 2+1 bullet clock and absurd on a 1+10 one,
	// where it would swallow the whole clock and make the engine think
	// less with an increment than without one.
	reserveMs := float64(baseReserveMs + reserveIncrements*incMs)
	if maxReserve := float64(remainMs) * maxReserveShare; reserveMs > maxReserve {
		reserveMs = maxReserve
	}
	// Capping the reserve at a quarter keeps this at three quarters of the
	// clock or more, so it never goes negative and the scramble is handled
	// by the floor below rather than by a separate branch.
	usable := float64(remainMs) - reserveMs
	if budgetMs > usable {
		budgetMs = usable
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
