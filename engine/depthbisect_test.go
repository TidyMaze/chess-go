package engine

import (
	"testing"
)

// Which search feature makes a deeper search play worse?
//
// Depth 6 loses to depth 5 by 125 to 167 Elo with the same evaluation on
// both sides, which no correct search does. Scores across depths look
// sane, so the fault is in move selection rather than evaluation. This
// disables one feature at a time on BOTH sides and re-runs the same
// comparison: whichever removal makes the deficit disappear is the cause.
//
// Run with: go test ./engine/ -run TestDepthBisect -v -timeout 60m
func TestDepthBisect(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	const games = 160

	cases := []struct {
		name string
		mod  func(*Player)
	}{
		{"baseline (nothing disabled)", func(p *Player) {}},
		{"no aspiration", func(p *Player) { p.Aspiration = false }},
		{"no LMR", func(p *Player) { p.NoLMR = true }},
		{"no futility", func(p *Player) { p.Futility = false }},
		{"no null move", func(p *Player) { p.NullMove = false }},
		{"no extensions", func(p *Player) { p.Extensions = false }},
		{"no SEE pruning", func(p *Player) { p.SEEPruning = false }},
		{"no transposition table", func(p *Player) { p.TTBits = 0 }},
	}
	for _, c := range cases {
		deep, shallow := Strong(6), Strong(5)
		c.mod(&deep)
		c.mod(&shallow)
		deep.Name, shallow.Name = "depth6", "depth5"
		res := PlayMatch(deep, shallow, games, 250)
		t.Logf("%-28s depth6 vs depth5: W-D-L %d-%d-%d  Elo %+d +/- %d",
			c.name, res.Wins, res.Draws, res.Losses, res.Elo(), res.EloMargin())
	}
}
