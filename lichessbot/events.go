package lichessbot

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// The two events on the account-wide stream (GET /api/stream/event) that
// this bot acts on. Lichess sends others (gameFinish, challengeCanceled,
// challengeDeclined); those are logged and otherwise ignored.
type accountEvent struct {
	Type          string          `json:"type"`
	ChallengeData json.RawMessage `json:"challenge"`
	Game          struct {
		ID string `json:"id"`
	} `json:"game"`
}

type challengeWire struct {
	ID      string `json:"id"`
	Rated   bool   `json:"rated"`
	Speed   string `json:"speed"`
	Variant struct {
		Key string `json:"key"`
	} `json:"variant"`
	Challenger struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"challenger"`
}

// isOutgoing decides whether this is a challenge we sent ourselves, echoed
// back on the same account stream that carries incoming ones, by matching
// the challenger's id against our own username. Lichess's stream event
// carries no "direction" field, unlike the POST response that creates a
// challenge; matching ids is the only signal available here.
func (c challengeWire) isOutgoing(ourUsername string) bool {
	return c.Challenger.ID == ourUsername
}

func (c challengeWire) toChallenge(ourUsername string) Challenge {
	return Challenge{
		ID:       c.ID,
		Variant:  c.Variant.Key,
		Rated:    c.Rated,
		SpeedTC:  c.Speed,
		FromBot:  c.Challenger.Title == "BOT",
		Outgoing: c.isOutgoing(ourUsername),
	}
}

// The two events on a game stream (GET /api/bot/game/stream/{id}).
type gameFull struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	White struct {
		ID string `json:"id"`
	} `json:"white"`
	Black struct {
		ID string `json:"id"`
	} `json:"black"`
	InitialFen string `json:"initialFen"`
	State      gameState
}

// WhiteTimeMS, BlackTimeMS, WhiteIncMS and BlackIncMS are the live clock
// lichess sends with every gameState (and inside gameFull.state): time
// left and increment, in milliseconds, for each side. A correspondence
// game, which has no clock, sends none of these, so they read zero.
type gameState struct {
	Type        string `json:"type"`
	Moves       string `json:"moves"`
	Status      string `json:"status"`
	WhiteTimeMS int64  `json:"wtime"`
	BlackTimeMS int64  `json:"btime"`
	WhiteIncMS  int64  `json:"winc"`
	BlackIncMS  int64  `json:"binc"`
}

// ourColor decides which side we are in a game, by comparing our own
// lichess username (lowercased, as lichess sends ids) against white and
// black's ids in the gameFull event.
func ourColor(g gameFull, ourUsername string) (string, error) {
	switch ourUsername {
	case g.White.ID:
		return "white", nil
	case g.Black.ID:
		return "black", nil
	default:
		return "", fmt.Errorf("neither side of game %s is %q (white=%q black=%q)",
			g.ID, ourUsername, g.White.ID, g.Black.ID)
	}
}

// gameOver reports whether a status string from lichess means the game
// has ended, so the stream loop for that game can stop.
func gameOver(status string) bool {
	switch status {
	case "created", "started":
		return false
	default:
		return true
	}
}

// moveForm builds the form body for POST /api/bot/game/{id}/move/{uci},
// including a draw offer only when asked.
func moveForm(offerDraw bool) string {
	if offerDraw {
		return url.Values{"offeringDraw": {"true"}}.Encode()
	}
	return ""
}
