package main

import (
	"testing"

	"chess/game"
)

func mv(t *testing.T, uci string) game.Move {
	t.Helper()
	m, ok := game.MoveFromUCI(uci)
	if !ok {
		t.Fatalf("MoveFromUCI(%q) failed", uci)
	}
	return m
}

func TestClassifyLabelsWhatAMoveDoes(t *testing.T) {
	const start = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
	if k := classify(start, mv(t, "e2e4")); !k["pawn push"] || k["capture"] || k["develops"] {
		t.Errorf("e2e4 from the start is a plain pawn push, got %v", k)
	}
	if k := classify(start, mv(t, "g1f3")); !k["develops"] || k["pawn push"] {
		t.Errorf("g1f3 develops a knight, got %v", k)
	}
	// White queen takes f7 with check, from a position where that is legal.
	const scholars = "r1bqkbnr/pppp1ppp/2n5/4p3/2B1P3/8/PPPP1PPP/RNBQK1NR w KQkq - 0 1"
	k := classify(scholars, mv(t, "c4f7"))
	if !k["capture"] || !k["check"] {
		t.Errorf("Bxf7+ is a capture and a check, got %v", k)
	}
	// A black pawn on its own sixth rank counts as advanced, which for
	// Black means a low rank number.
	const advanced = "8/8/8/8/8/2p5/8/K6k b - - 0 1"
	if k := classify(advanced, mv(t, "c3c2")); !k["pawn to 6th or 7th"] {
		t.Errorf("a black pawn reaching the seventh is advanced, got %v", k)
	}
}

func TestKindGapCountsWhoPlayedWhat(t *testing.T) {
	const start = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
	g := newKindGap()
	// We push a pawn, the judge develops: one gap each way.
	g.add(start, mv(t, "a2a3"), mv(t, "g1f3"))
	if g.judgeOnly["develops"] != 1 {
		t.Errorf("the judge developed and we did not, got %v", g.judgeOnly)
	}
	if g.ourOnly["pawn push"] != 1 {
		t.Errorf("we pushed a pawn and the judge did not, got %v", g.ourOnly)
	}
	if g.judgeOnly["capture"] != 0 || g.ourOnly["capture"] != 0 {
		t.Error("neither move was a capture, so captures must not be counted")
	}
}
