package engine

import (
	"chess/game"
	"math"
	"testing"
)

func TestPlayerPickScoredReturnsMoveAndScore(t *testing.T) {
	g := game.New()
	p := Strong(2)
	m, score, ok := PlayerPickScored(p, g)
	if !ok {
		t.Fatal("expected ok to be true")
	}
	if m.From == m.To {
		t.Fatal("expected non-empty move")
	}
	if math.IsNaN(score) || math.IsInf(score, 0) {
		t.Fatalf("expected valid numeric score, got %v", score)
	}
}
