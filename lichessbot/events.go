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
		Title string `json:"title"`
	} `json:"challenger"`
}

func (c challengeWire) toChallenge() Challenge {
	return Challenge{
		ID:      c.ID,
		Variant: c.Variant.Key,
		Rated:   c.Rated,
		SpeedTC: c.Speed,
		FromBot: c.Challenger.Title == "BOT",
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

type gameState struct {
	Type   string `json:"type"`
	Moves  string `json:"moves"`
	Status string `json:"status"`
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
