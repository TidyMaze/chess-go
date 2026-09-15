package main

import (
	"strings"
	"testing"

	"chess/engine"
	"chess/game"
)

func TestDefaultSeedOpeningsAreLegal(t *testing.T) {
	for i, seed := range defaultSeedOpenings {
		g := game.New()
		moves := strings.Fields(seed)
		for _, mvStr := range moves {
			m, ok := game.MoveFromUCI(mvStr)
			if !ok {
				t.Fatalf("seed %d (%q): invalid UCI move %q", i, seed, mvStr)
			}
			if !g.IsLegalMove(m) {
				t.Fatalf("seed %d (%q): move %q is not legal in position %s", i, seed, mvStr, g.FEN())
			}
			g.ApplyMove(m.From, m.To)
		}
	}
}

func TestSelectOpponentMoves(t *testing.T) {
	g := game.New()
	p := engine.Strong(1)

	// In initial position, White has 20 legal moves
	candidates := selectOpponentMoves(g, p, 4)
	if len(candidates) != 4 {
		t.Fatalf("expected 4 candidates, got %d", len(candidates))
	}
	for _, m := range candidates {
		if !g.IsLegalMove(m) {
			t.Fatalf("candidate %v is not legal", m)
		}
	}
}
