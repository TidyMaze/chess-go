package engine

import (
	"testing"

	"chess/game"
)

// moveaudit found that the engine reaches for pieces where a strong
// search reaches for pawns, and the first explanation offered was that
// the network cannot see pawn structure. It can, and these are the
// numbers that refuted it, kept so a retrained network cannot quietly
// lose the knowledge.
//
// Each pair holds the same material for both sides and differs only in
// how the pawns stand, so any difference in the score is structural.
func TestNetworkSeesPawnStructure(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no trained network available")
	}
	eval := func(fen string) float64 {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatalf("ParseFEN(%q): %v", fen, err)
		}
		return net.Evaluate(&g.Board)
	}
	cases := []struct {
		name          string
		sound, broken string
		// The least the second position must score below the first, in
		// pawns. Recorded well under what the champion network gives, so
		// this catches knowledge being lost, not ordinary retraining drift.
		atLeast float64
	}{
		{
			name:    "doubling and isolating White's pawns, three against three",
			sound:   "4k3/ppp5/8/8/8/8/PPP5/4K3 w - - 0 1",
			broken:  "4k3/ppp5/8/8/8/P7/P1P5/4K3 w - - 0 1",
			atLeast: 0.20,
		},
		{
			name:    "blocking White's passed pawn, two against two",
			sound:   "4k3/1p6/8/P7/8/8/1P6/4K3 w - - 0 1",
			broken:  "4k3/8/1p6/Pp6/8/8/1P6/4K3 w - - 0 1",
			atLeast: 0.50,
		},
	}
	for _, c := range cases {
		sound, broken := eval(c.sound), eval(c.broken)
		if sound-broken < c.atLeast {
			t.Errorf("%s\n  sound %+.3f, broken %+.3f, difference %+.3f, want at least %.2f",
				c.name, sound, broken, sound-broken, c.atLeast)
		}
	}
}
