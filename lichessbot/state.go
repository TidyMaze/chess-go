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
		// No clock at all (correspondence) or a clock that has not been
		// reported yet: let the caller fall back to a fixed budget.
		return 0
	}
	const (
		fractionOfRemaining = 1.0 / 30.0
		fractionOfIncrement = 0.8
		safetyMarginMs      = 200
		minBudgetMs         = 50
		maxBudgetMs         = 15000
	)
	budgetMs := float64(remainMs)*fractionOfRemaining + float64(incMs)*fractionOfIncrement
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
