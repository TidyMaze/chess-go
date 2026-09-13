package lichessbot

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"

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
}

// Run reads the account event stream until it ends (the connection drops
// or the process is asked to stop). Each accepted challenge's game is
// played in its own goroutine so a slow or long game never blocks the bot
// from accepting the next challenge.
func (b *Bot) Run() error {
	stream, err := b.API.streamNDJSON("/api/stream/event")
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
	stream, err := b.API.streamNDJSON("/api/bot/game/stream/" + gameID)
	if err != nil {
		b.logf("game %s: stream failed: %v", gameID, err)
		return
	}
	defer stream.Close()

	var full gameFull
	haveFull := false

	err = eachLine(stream, func(line []byte) error {
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
func effectivePlayer(base engine.Player, ourColor string, st gameState) engine.Player {
	if budget := moveTimeBudget(ourColor, st); budget > 0 {
		base.TimeBudget = budget
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
	m, ok := engine.PlayerPick(effectivePlayer(b.Player, color, st), g)
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
