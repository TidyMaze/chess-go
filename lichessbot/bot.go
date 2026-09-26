package lichessbot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"chess/engine"
	"chess/game"
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
	// RateLimitWait is the pause before any request that follows a 429. Zero
	// means the minute lichess asks for.
	RateLimitWait time.Duration

	gamesInPlay atomic.Int32
	// active holds the id of every game a playGame loop is running for.
	// Lichess announces every ongoing game again as gameStart each time the
	// event stream connects, so without it a reconnect starts a second loop
	// for a game already being played: two searches per move on the same
	// cores, two posts, and one game counted twice against MaxGames.
	active sync.Map
}

// gameSession is what one game carries from one move to the next.
type gameSession struct {
	// One estimate per game: the overhead is a property of who is on the
	// other side, so it must not be shared between games or carried across
	// them.
	overhead *overheadEstimate
	// One transposition table per game, so subsequent moves in the same game
	// reuse previously explored branches rather than re-allocating 24-96 MB
	// every move.
	table *engine.TranspositionTable

	// The rest is shared with a move post being retried, hence the lock.
	mu sync.Mutex
	// claimed is the move list the bot is answering or has answered. An
	// opponent who offers a draw or proposes a takeback while the bot thinks
	// makes lichess queue a gameState with that same list; searching it
	// again blocks the stream while the real reply waits on the bot's clock,
	// and the stale move is then refused, or accepted as the answer to a
	// position the bot never looked at.
	claimed  string
	hasClaim bool
	// latest is the newest state read from the game stream, at latestAt.
	latest   gameState
	latestAt time.Time
}

func (s *gameSession) observe(st gameState, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest, s.latestAt = st, at
}

// outageDeadline is the last moment the game can still be running while the
// bot cannot reach it: both clocks of the newest state read, run down one
// after the other from when it was read, since nobody gains time without
// moving and the bot cannot move. It is never earlier than window after the
// stream last worked, which also covers a game whose clocks are not known.
func (s *gameSession) outageDeadline(lastWorked time.Time, window time.Duration) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	deadline := lastWorked.Add(window)
	if !s.latestAt.IsZero() {
		clocks := time.Duration(s.latest.WhiteTimeMS+s.latest.BlackTimeMS) * time.Millisecond
		if byClock := s.latestAt.Add(clocks); byClock.After(deadline) {
			deadline = byClock
		}
	}
	return deadline
}

func (s *gameSession) isClaimed(moves string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasClaim && s.claimed == moves
}

func (s *gameSession) claim(moves string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimed, s.hasClaim = moves, true
}

// release gives the claim on moves back once its move turned out not to be
// posted, so a later event with the same moves, such as a reconnect's
// gameFull, may try again.
func (s *gameSession) release(moves string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimed == moves {
		s.hasClaim = false
	}
}

// stillExpects reports whether the game, as the stream last showed it, is
// still waiting for the move that answers moves.
func (s *gameSession) stillExpects(moves string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !gameOver(s.latest.Status) && strings.TrimSpace(s.latest.Moves) == moves
}

const (
	defaultReconnectDelay    = time.Second
	maxReconnectDelay        = time.Minute
	defaultHealthyConnection = 2 * time.Minute
	// How many connections to a game's stream may hand over nothing at all
	// before the bot accepts that lichess no longer serves that game.
	maxEmptyGameStreams = 3

	// A move post that fails without lichess refusing it (a dropped request,
	// a 502, a timeout) is made again, because nothing else will: lichess
	// sends no event until someone moves, so a lost post leaves the bot on a
	// silent stream until its flag falls. The wait doubles from the first
	// delay up to the cap, for as long as the bot's clock lasts, or for the
	// window when lichess reports no clock for it.
	firstMoveRetryDelay    = 100 * time.Millisecond
	maxMoveRetryDelay      = time.Second
	noClockMoveRetryWindow = time.Minute

	// Lichess on a 429: "waiting one minute before retrying will be
	// sufficient". The limit is per token, so a request made sooner prolongs
	// it for every game in play and the event stream.
	defaultRateLimitWait = time.Minute
)

func (b *Bot) rateLimitWait() time.Duration {
	if b.RateLimitWait > 0 {
		return b.RateLimitWait
	}
	return defaultRateLimitWait
}

// waitAfter is wait, stretched to the rate-limit pause when err is a 429.
func (b *Bot) waitAfter(err error, wait time.Duration) time.Duration {
	if isRateLimited(err) {
		return max(wait, b.rateLimitWait())
	}
	return wait
}

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
	if _, running := b.active.LoadOrStore(gameID, struct{}{}); running {
		b.logf("game %s: already playing it, ignoring the repeated gameStart", gameID)
		return
	}
	defer b.active.Delete(gameID)
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
	sess := &gameSession{overhead: newOverheadEstimate()}
	if b.Player.TTBits > 0 {
		sess.table = engine.NewTranspositionTable(b.Player.TTBits)
	}
	gameCtx, dropGame := context.WithCancel(ctx)
	defer dropGame()

	// The game outlives any one connection to it. A stream that drops is a
	// dropped socket, not a finished game, and the only thing that ends a
	// game is lichess saying so. Reading once and returning is how game
	// rUZ1clR4 was lost on time: its stream died after 2m24s with errno
	// 65535, this function logged "finished", and the bot then sat on a live
	// board hearing nothing until the flag fell.
	var full gameFull
	haveFull := false
	empties := 0
	lastWorked := time.Now()
	outageDelay := b.reconnectDelay()
	for {
		if gameCtx.Err() != nil {
			return
		}
		lines, opened, over, err := b.readGameStream(gameCtx, gameID, &full, &haveFull, sess)
		if over {
			return
		}
		wait := b.reconnectDelay()
		switch {
		case lines > 0:
			// Progress is the thing worth retrying on: a connection that
			// handed over even one line is a game lichess still knows about.
			empties = 0
			lastWorked = time.Now()
			outageDelay = b.reconnectDelay()
		case opened || isDefinitive(err):
			// Several connections in a row that hand over nothing, or that
			// lichess refuses outright (a 404), are a game it has forgotten,
			// and retrying those forever would leave a goroutine spinning for
			// the life of the process.
			if empties++; empties >= maxEmptyGameStreams {
				b.logf("game %s: giving up after %d connections that delivered nothing", gameID, empties)
				return
			}
		default:
			// Not reached at all: the network is down, or lichess is
			// restarting. That says nothing about the game, which goes on
			// running on the clocks, so it is tried again with a growing
			// wait until the clocks read last have both run out. Three tries
			// a second apart used to abandon a live game after two seconds of
			// outage.
			if time.Now().After(sess.outageDeadline(lastWorked, b.maxReconnectDelay())) {
				b.logf("game %s: giving up: no connection since %s, and the game clocks have run out",
					gameID, lastWorked.Format(time.TimeOnly))
				return
			}
			wait = b.waitAfter(err, outageDelay)
			outageDelay = b.nextReconnectDelay(outageDelay, 0)
		}
		if err != nil && err != io.EOF {
			b.logf("game %s: stream ended: %v; reconnecting", gameID, err)
		}
		select {
		case <-gameCtx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// readGameStream reads one connection to a game's stream to its end. It
// reports how many lines it managed to read and whether the connection opened
// at all, so the caller can tell a dropped connection or an outage from a
// game lichess no longer serves, and whether the game is over, which is the
// only reason to stop reconnecting.
func (b *Bot) readGameStream(ctx context.Context, gameID string, full *gameFull, haveFull *bool, sess *gameSession) (lines int, opened, over bool, err error) {
	stream, err := b.API.streamNDJSON(ctx, "/api/bot/game/stream/"+gameID)
	if err != nil {
		b.logf("game %s: stream failed: %v", gameID, err)
		return 0, false, false, err
	}
	defer stream.Close()

	err = eachLine(stream, func(line []byte) error {
		// Stamped before anything is parsed: the clock has been running
		// since lichess registered the opponent's move, and everything from
		// here to the move being posted is charged to it.
		received := time.Now()
		lines++
		kind, err := parseKind(line)
		if err != nil {
			return nil
		}
		switch kind {
		case "gameFull":
			if err := json.Unmarshal(line, full); err != nil {
				return nil
			}
			*haveFull = true
			over = over || gameOver(full.State.Status)
			b.maybeMove(ctx, gameID, *full, full.State, received, sess)
		case "gameState":
			if !*haveFull {
				return nil
			}
			var st gameState
			if err := json.Unmarshal(line, &st); err != nil {
				return nil
			}
			over = over || gameOver(st.Status)
			b.maybeMove(ctx, gameID, *full, st, received, sess)
		}
		return nil
	})
	return lines, true, over, err
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

// maybeMove answers st when it is our turn. ctx is the game's: it bounds a
// move post being retried after maybeMove has returned.
func (b *Bot) maybeMove(ctx context.Context, gameID string, full gameFull, st gameState, received time.Time, sess *gameSession) {
	sess.observe(st, received)
	if gameOver(st.Status) {
		return
	}
	moves := strings.TrimSpace(st.Moves)
	if sess.isClaimed(moves) {
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
	sess.claim(moves)
	player := effectivePlayer(b.Player, color, st, full.Speed, sess.overhead)
	searchStart := time.Now()
	var m game.Move
	var ok bool
	if sess.table != nil {
		m, ok = engine.PlayerPickWith(player, g, sess.table)
	} else {
		m, ok = engine.PlayerPick(player, g)
	}
	searched := time.Since(searchStart)
	if !ok {
		b.logf("game %s: no legal move found on our turn", gameID)
		return
	}
	uci := g.MoveUCI(m)
	path := "/api/bot/game/" + gameID + "/move/" + url.PathEscape(uci)
	postStart := time.Now()
	err = b.API.postForm(path, "")
	posted := time.Since(postStart)
	// Feed it back, so the next move of this game budgets for what this one
	// actually cost rather than for what a constant guessed.
	sess.overhead.observe(posted)
	if err != nil {
		b.logf("game %s: move %s failed: %v", gameID, uci, err)
		if isDefinitive(err) {
			sess.release(moves)
			return
		}
		// Retried off the stream's goroutine, so the stream keeps being read
		// and can show whether the game still waits for this move.
		go b.retryMove(ctx, gameID, uci, path, moves, flagFalls(color, st, received), sess, err)
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

// flagFalls is when our clock runs out if we never move, going by the state
// received at at. With no clock reported, it is the fixed retry window.
func flagFalls(ourColor string, st gameState, at time.Time) time.Time {
	ms := st.WhiteTimeMS
	if ourColor == "black" {
		ms = st.BlackTimeMS
	}
	if ms <= 0 {
		return at.Add(noClockMoveRetryWindow)
	}
	return at.Add(time.Duration(ms) * time.Millisecond)
}

// retryMove posts a move whose post failed again, with a doubling wait, for
// as long as the game still waits for it and the clock leaves time for it.
// It stops at once on a refusal from lichess, which no retry will change, and
// waits out a rate limit before the next try. err is how the last post failed.
func (b *Bot) retryMove(ctx context.Context, gameID, uci, path, moves string, flag time.Time, sess *gameSession, err error) {
	posted := false
	defer func() {
		if !posted {
			sess.release(moves)
		}
	}()
	for delay := firstMoveRetryDelay; ; delay = min(2*delay, maxMoveRetryDelay) {
		wait := b.waitAfter(err, delay)
		if time.Now().Add(wait).After(flag) {
			b.logf("game %s: move %s not posted, and the clock runs out before the next try", gameID, uci)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if !sess.stillExpects(moves) {
			b.logf("game %s: move %s: the game has moved on, not posting it again", gameID, uci)
			return
		}
		err = b.API.postForm(path, "")
		if err == nil {
			posted = true
			b.logf("game %s: %s posted on a retry", gameID, uci)
			return
		}
		b.logf("game %s: move %s failed again: %v", gameID, uci, err)
		if isDefinitive(err) {
			return
		}
	}
}

func (b *Bot) logf(format string, args ...any) {
	if b.Log != nil {
		b.Log.Printf(format, args...)
		return
	}
	fmt.Printf(format+"\n", args...)
}
