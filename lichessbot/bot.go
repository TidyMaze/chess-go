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

	// IdleTimeout is how long a stream may say nothing at all, keepalive
	// blank lines included, before the bot treats it as dead and reconnects.
	// ReconnectDelay is the first wait between attempts, doubling up to
	// maxReconnectDelay so a lichess outage is not hammered. Both are fields
	// rather than constants because the tests drive them at millisecond
	// scale, and because a real network wants a knob.
	IdleTimeout       time.Duration
	ReconnectDelay    time.Duration
	MaxReconnectDelay time.Duration

	gamesInPlay atomic.Int32
}

// Defaults for the two knobs above. Lichess sends a keepalive every few
// seconds, so a minute of pure silence is generous and still notices a dead
// socket long before a human would.
const (
	defaultIdleTimeout    = 60 * time.Second
	defaultReconnectDelay = time.Second
	maxReconnectDelay     = time.Minute
)

func (b *Bot) idleTimeout() time.Duration {
	if b.IdleTimeout > 0 {
		return b.IdleTimeout
	}
	return defaultIdleTimeout
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
		if err != nil && ctx.Err() == nil {
			b.logf("event stream ended: %v", err)
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
	if lasted > 2*b.idleTimeout() {
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
	stream, err := b.API.streamNDJSON("/api/stream/event")
	if err != nil {
		return err
	}
	defer stream.Close()

	// Cancelling must break the read, and closing the stream is the only
	// thing that does: the read is blocked inside the connection.
	watch := newIdleReader(stream, b.idleTimeout())
	defer watch.Close()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			watch.Close()
		case <-stopped:
		}
	}()

	return eachLine(watch, func(line []byte) error {
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
			go b.playGame(e.Game.ID)
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
func (b *Bot) playGame(gameID string) {
	b.gamesInPlay.Add(1)
	defer b.gamesInPlay.Add(-1)
	stream, err := b.API.streamNDJSON("/api/bot/game/stream/" + gameID)
	if err != nil {
		b.logf("game %s: stream failed: %v", gameID, err)
		return
	}
	defer stream.Close()

	// A game stream that dies silently is worse than a dead event stream:
	// this goroutine holds one of MaxGames slots for as long as it blocks,
	// so enough leaked games would have the bot decline every challenge
	// while looking busy. Same watchdog, same reason.
	watch := newIdleReader(stream, b.idleTimeout())
	defer watch.Close()

	var full gameFull
	haveFull := false

	err = eachLine(watch, func(line []byte) error {
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
			b.maybeMove(gameID, full, full.State)
		case "gameState":
			if !haveFull {
				return nil
			}
			var st gameState
			if err := json.Unmarshal(line, &st); err != nil {
				return nil
			}
			b.maybeMove(gameID, full, st)
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
func effectivePlayer(base engine.Player, ourColor string, st gameState, speed string) engine.Player {
	if budget := moveTimeBudget(ourColor, st); budget > 0 {
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

func (b *Bot) maybeMove(gameID string, full gameFull, st gameState) {
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
	m, ok := engine.PlayerPick(effectivePlayer(b.Player, color, st, full.Speed), g)
	if !ok {
		b.logf("game %s: no legal move found on our turn", gameID)
		return
	}
	uci := moveUCIForLichess(g, m)
	if err := b.API.postForm("/api/bot/game/"+gameID+"/move/"+url.PathEscape(uci), ""); err != nil {
		b.logf("game %s: move %s failed: %v", gameID, uci, err)
	}
}

func (b *Bot) logf(format string, args ...any) {
	if b.Log != nil {
		b.Log.Printf(format, args...)
		return
	}
	fmt.Printf(format+"\n", args...)
}
