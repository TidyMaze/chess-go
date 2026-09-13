package lichessbot

import "testing"

func TestChallengeWireToChallenge(t *testing.T) {
	var c challengeWire
	c.ID = "abc123"
	c.Rated = true
	c.Speed = "blitz"
	c.Variant.Key = "standard"
	c.Challenger.Title = "BOT"

	got := c.toChallenge()
	want := Challenge{ID: "abc123", Variant: "standard", Rated: true, SpeedTC: "blitz", FromBot: true}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestChallengeWireNonBotChallenger(t *testing.T) {
	var c challengeWire
	c.Challenger.Title = ""
	if c.toChallenge().FromBot {
		t.Error("an empty title was read as a BOT challenger")
	}
}

func TestOurColorMatchesWhite(t *testing.T) {
	var g gameFull
	g.ID = "g1"
	g.White.ID = "tidymazebot"
	g.Black.ID = "someoneelse"
	color, err := ourColor(g, "tidymazebot")
	if err != nil {
		t.Fatal(err)
	}
	if color != "white" {
		t.Errorf("got %q, want white", color)
	}
}

func TestOurColorMatchesBlack(t *testing.T) {
	var g gameFull
	g.ID = "g1"
	g.White.ID = "someoneelse"
	g.Black.ID = "tidymazebot"
	color, err := ourColor(g, "tidymazebot")
	if err != nil {
		t.Fatal(err)
	}
	if color != "black" {
		t.Errorf("got %q, want black", color)
	}
}

// If our own username matches neither side, playing on would send moves
// into someone else's game; refusing loudly beats guessing a color.
func TestOurColorErrorsWhenNeitherSideMatches(t *testing.T) {
	var g gameFull
	g.ID = "g1"
	g.White.ID = "alice"
	g.Black.ID = "bob"
	if _, err := ourColor(g, "tidymazebot"); err == nil {
		t.Error("expected an error when our username matches neither side")
	}
}

func TestGameOverStatuses(t *testing.T) {
	for _, s := range []string{"created", "started"} {
		if gameOver(s) {
			t.Errorf("status %q was reported as over", s)
		}
	}
	for _, s := range []string{"mate", "resign", "aborted", "draw", "outoftime", "timeout", "stalemate"} {
		if !gameOver(s) {
			t.Errorf("status %q was not reported as over", s)
		}
	}
}

func TestMoveFormWithAndWithoutDrawOffer(t *testing.T) {
	if got := moveForm(false); got != "" {
		t.Errorf("no draw offer should produce an empty body, got %q", got)
	}
	if got := moveForm(true); got != "offeringDraw=true" {
		t.Errorf("got %q, want offeringDraw=true", got)
	}
}
