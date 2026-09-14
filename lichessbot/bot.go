package lichessbot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"sync/atomic"
	"time"

	"chess/engine"
)

// Bot drives the champion against lichess: it reads the account event
// stream, accepts standard-chess challenges, and plays each accepted game
// by asking the champion for a move whenever the game stream says it is
// our turn. Every move is PlayerPickWith on the champion, the same call
// every other measurement in this repo makes; nothing here scores a
// position with anything but the engine's own search.
type Bot struct {
	API      api
	Player   engine.Player
	Username string // lowercase, as lichess reports it in game.white.id
	Log      *log.Logger
	// MaxGames caps how many games are played at once. Zero means no cap.
	//
	// Each game runs its own search, and the champion asks for 8 threads,
	// so games in flight multiply: seven at once is 56 search threads on a
	// machine with 4 performance cores, and every one of them then searches
	// far shallower than the champion was ever calibrated at. Past the cap
	// a challenge is declined rather than accepted and played badly.
	MaxGames int

	// ReconnectDelay is the first wait between connection attempts, doubling
	// up to MaxReconnectDelay so a lichess outage is not hammered. Fields
	// rather than constants because the tests drive them at millisecond
	// scale, and because a real network wants a knob.
	//
	// HealthyConnection is how long a connection must last to count as
	// healthy, which resets the backoff. Noticing a dead connection is the
	// transport's job, not this struct's: see the ping health check in
	// client.go for why silence cannot be used to judge a stream.
	ReconnectDelay    time.Duration
	MaxReconnectDelay time.Duration
	HealthyConnection time.Duration

	gamesInPlay atomic.Int32
}

const (
	defaultReconnectDelay    = time.Second
	maxReconnectDelay        = time.Minute
	defaultHealthyConnection = 2 * time.Minute
)

func (b *Bot) healthyConnection() time.Duration {
	if b.HealthyConnection > 0 {
		return b.HealthyConnection
	}
	return defaultHealthyConnection
}

func (b *Bot) reconnectDelay() time.Duration {
	if b.ReconnectDelay > 0 {
		return b.ReconnectDelay
	}
	return defaultReconnectDelay
}

func (b *Bot) maxReconnectDelay() time.Duration {
	if b.MaxReconnectDelay > 0 {
		return b.MaxReconnectDelay
	}
	return maxReconnectDelay
}

// Run keeps the bot on the account event stream until ctx is cancelled,
// reconnecting whenever the stream ends. It returns only on cancellation:
// every other outcome, a clean end, a dropped socket, a refused connection,
// is temporary and must not take the bot off lichess for the rest of the
// day. A single pass used to be the whole of Run, which meant the first
// time lichess closed the stream the bot stopped playing and said nothing.
func (b *Bot) Run(ctx context.Context) error {
	delay := b.reconnectDelay()
	for {
		if ctx.Err() != nil {
			return nil
		}
		start := time.Now()
		err := b.runOnce(ctx)
		lasted := time.Since(start)
		// Logged even when the stream ended cleanly. Saying nothing on a
		// clean end is how this went unnoticed: the bot sat with an empty
		// log for twelve minutes and nothing said it had stopped.
		if ctx.Err() == nil {
			if err != nil {
				b.logf("event stream ended after %s: %v; reconnecting in %s", lasted.Round(time.Millisecond), err, delay)
			} else {
				b.logf("event stream ended after %s; reconnecting in %s", lasted.Round(time.Millisecond), delay)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		delay = b.nextReconnectDelay(delay, lasted)
	}
}

// nextReconnectDelay doubles the wait up to the cap, and starts the backoff
// over when the connection that just ended had lasted long enough to count
// as healthy: an outage that is already finished should not leave the bot
// reconnecting slowly for the rest of the day. Pure, so the schedule can be
// tested without waiting on it.
func (b *Bot) nextReconnectDelay(current, lasted time.Duration) time.Duration {
	if lasted > b.healthyConnection() {
		return b.reconnectDelay()
	}
	if doubled := current * 2; doubled < b.maxReconnectDelay() {
		return doubled
	}
	return b.maxReconnectDelay()
}

// runOnce reads the account event stream for as long as one connection
// lasts. Each accepted challenge's game is played in its own goroutine so a
// slow or long game never blocks the bot from accepting the next challenge.
func (b *Bot) runOnce(ctx context.Context) error {
	// The connection gets a context of its own so cancelling the bot aborts
	// a request that has not answered yet, and so ending this stream never
	// touches games already in flight.
	connCtx, dropConn := context.WithCancel(ctx)
	defer dropConn()
	stream, err := b.API.streamNDJSON(connCtx, "/api/stream/event")
	if err != nil {
		return err
	}
	defer stream.Close()

	return eachLine(stream, func(line []byte) error {
		kind, err := parseKind(line)
		if err != nil {
			b.logf("unreadable event: %v", err)
			return nil
		}
		switch kind {
		case "challenge":
			b.handleChallenge(line)
		case "gameStart":
			var e accountEvent
			if err := json.Unmarshal(line, &e); err != nil {
				b.logf("unreadable gameStart: %v", err)
				return nil
			}
			// Games run off the bot's own context, not this
			// connection's, so reconnecting the event stream never
			// abandons a game in progress.
			go b.playGame(ctx, e.Game.ID)
		}
		return nil
	})
}

func (b *Bot) handleChallenge(line []byte) {
	var e struct {
		Challenge challengeWire `json:"challenge"`
	}
	if err := json.Unmarshal(line, &e); err != nil {
		b.logf("unreadable challenge: %v", err)
		return
	}
	c := e.Challenge.toChallenge(b.Username)
	if c.Outgoing {
		// Lichess echoes our own outgoing challenges on this stream too;
		// there is no accept or decline action for one we sent ourselves.
		return
	}
	if b.MaxGames > 0 && int(b.gamesInPlay.Load()) >= b.MaxGames {
		b.logf("declining challenge %s: already playing %d games", c.ID, b.gamesInPlay.Load())
		if err := b.API.postForm("/api/challenge/"+c.ID+"/decline", ""); err != nil {
			b.logf("decline %s failed: %v", c.ID, err)
		}
		return
	}
	if !shouldAcceptChallenge(c) {
		b.logf("declining challenge %s (variant=%q speed=%q)", c.ID, c.Variant, c.SpeedTC)
		if err := b.API.postForm("/api/challenge/"+c.ID+"/decline", ""); err != nil {
			b.logf("decline %s failed: %v", c.ID, err)
		}
		return
	}
	b.logf("accepting challenge %s (variant=%q speed=%q rated=%v)", c.ID, c.Variant, c.SpeedTC, c.Rated)
	if err := b.API.postForm("/api/challenge/"+c.ID+"/accept", ""); err != nil {
		b.logf("accept %s failed: %v", c.ID, err)
	}
}

// playGame streams one game to completion. Every event carries the full
// move list to date, so the position is rebuilt from scratch each time
// rather than incrementally tracked; a dropped or reordered event then
// costs one extra replay instead of a desynced board.
func (b *Bot) playGame(ctx context.Context, gameID string) {
	inPlay := b.gamesInPlay.Add(1)
	defer b.gamesInPlay.Add(-1)
	// A game the bot plays without saying so is a game nobody can tell it is
	// playing. The only reason the log stayed empty through a whole blitz
	// game against Stockfish was that starting one logged nothing: lichess
	// replays ongoing games as gameStart on every connect, and that path
	// never went past the challenge handler.
	started := time.Now()
	b.logf("game %s: playing (%d in play)", gameID, inPlay)
	defer func() {
		b.logf("game %s: finished after %s", gameID, time.Since(started).Round(time.Second))
	}()
	// One estimate per game: the overhead is a property of who is on the
	// other side, so it must not be shared between games or carried across
	// them.
	overhead := newOverheadEstimate()
	gameCtx, dropGame := context.WithCancel(ctx)
	defer dropGame()
	stream, err := b.API.streamNDJSON(gameCtx, "/api/bot/game/stream/"+gameID)
	if err != nil {
		b.logf("game %s: stream failed: %v", gameID, err)
		return
	}
	defer stream.Close()

	var full gameFull
	haveFull := false

	err = eachLine(stream, func(line []byte) error {
		// Stamped before anything is parsed: the clock has been running
		// since lichess registered the opponent's move, and everything from
		// here to the move being posted is charged to it.
		received := time.Now()
		kind, err := parseKind(line)
		if err != nil {
			return nil
		}
		switch kind {
		case "gameFull":
			if err := json.Unmarshal(line, &full); err != nil {
				return nil
			}
			haveFull = true
			b.maybeMove(gameID, full, full.State, received, overhead)
		case "gameState":
			if !haveFull {
				return nil
			}
			var st gameState
			if err := json.Unmarshal(line, &st); err != nil {
				return nil
			}
			b.maybeMove(gameID, full, st, received, overhead)
		}
		return nil
	})
	if err != nil && err != io.EOF {
		b.logf("game %s: stream ended: %v", gameID, err)
	}
}

// effectivePlayer applies this move's own clock to the champion, when
// lichess sent one: the live wtime/btime/winc/binc on the game state,
// not the fixed budget champion.json carries for a measurement race.
// Depth, threads and the network are untouched; only how long the search
// is allowed to run changes, per move, every move.
func effectivePlayer(base engine.Player, ourColor string, st gameState, speed string, overhead *overheadEstimate) engine.Player {
	if budget := moveTimeBudget(ourColor, st, overhead); budget > 0 {
		base.TimeBudget = budget
		return base
	}
	if speed == "correspondence" {
		// No clock to spend, so the race budget champion.json carries is
		// the wrong one: it played lichess's Stockfish at its top level
		// on one second a move and lost, game N1ok1iNf.
		base.TimeBudget = unlimitedBudget
	}
	return base
}

func (b *Bot) maybeMove(gameID string, full gameFull, st gameState, received time.Time, overhead *overheadEstimate) {
	if gameOver(st.Status) {
		return
	}
	color, err := ourColor(full, b.Username)
	if err != nil {
		b.logf("game %s: %v", gameID, err)
		return
	}
	fen := full.InitialFen
	if fen == "" {
		fen = "startpos"
	}
	g, err := applyMovesString(fen, st.Moves)
	if err != nil {
		b.logf("game %s: %v", gameID, err)
		return
	}
	if !isOurTurn(g, color) {
		return
	}
	player := effectivePlayer(b.Player, color, st, full.Speed, overhead)
	searchStart := time.Now()
	m, ok := engine.PlayerPick(player, g)
	searched := time.Since(searchStart)
	if !ok {
		b.logf("game %s: no legal move found on our turn", gameID)
		return
	}
	uci := moveUCIForLichess(g, m)
	postStart := time.Now()
	err = b.API.postForm("/api/bot/game/"+gameID+"/move/"+url.PathEscape(uci), "")
	posted := time.Since(postStart)
	// Feed it back, so the next move of this game budgets for what this one
	// actually cost rather than for what a constant guessed.
	overhead.observe(posted)
	if err != nil {
		b.logf("game %s: move %s failed: %v", gameID, uci, err)
		return
	}
	// Split, because the clock charges for both and only one of them is the
	// search. Audited over sixteen real games, the median move cost 0.7 s to
	// 1.0 s more than its budget at every time control, which is most of a
	// bullet move and cannot be explained by a search that overruns by 7%.
	// Whether that sits in the search or in the round trip is not something
	// the finished game's clocks can answer, so it is measured here.
	// total is everything this process is responsible for. Whatever the
	// clock lost beyond it is lichess reaching us, which nothing here can
	// measure directly and nothing here can shorten either.
	b.logf("game %s: %s in %s search + %s post, %s total (budget %s)",
		gameID, uci, searched.Round(time.Millisecond), posted.Round(time.Millisecond),
		time.Since(received).Round(time.Millisecond),
		player.TimeBudget.Round(time.Millisecond))
}

func (b *Bot) logf(format string, args ...any) {
	if b.Log != nil {
		b.Log.Printf(format, args...)
		return
	}
	fmt.Printf(format+"\n", args...)
}
