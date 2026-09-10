package engine

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"chess/game"
)

// An external engine's score has to reach the adjudicator, and when the
// engine gives none the adjudicator must see "unknown", not zero. Zero is
// a level position, and with two UCI players reporting it on every ply the
// draw rule fired at ply 70 in every game that got there: 185 draws in 200
// between two builds two and a half plies apart.
func TestUCIPlayerScoreReachesTheAdjudicator(t *testing.T) {
	fake := func(name, answer string) *UCIEngine {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nwhile read -r line; do\n  case \"$line\" in\n    uci*) echo uciok;;\n    isready) echo readyok;;\n    go*) "+answer+";;\n    quit) exit 0;;\n  esac\ndone\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		e, err := NewStockfish(path, 20, 0)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	g := game.New()
	cases := []struct {
		name, answer string
		want         float64
	}{
		{"cp", `echo "info depth 3 score cp 250 nodes 10"; echo "bestmove e2e4"`, 2.5},
		{"last info wins", `echo "info depth 2 score cp 10"; echo "info depth 3 score cp -40"; echo "bestmove e2e4"`, -0.4},
		{"mate for the mover", `echo "info depth 5 score mate 3"; echo "bestmove e2e4"`, 100},
		{"mated", `echo "info depth 5 score mate -2"; echo "bestmove e2e4"`, -100},
		{"garbled scores are ignored", `echo "info depth 1 score "; echo "info score cp x"; echo "info score lowerbound 3"; echo "info depth 2 score cp 70"; echo "bestmove e2e4"`, 0.7},
	}
	for _, c := range cases {
		p := Player{UCI: fake(c.name+".sh", c.answer)}
		_, score, ok := p.pickScored(g, nil)
		if !ok || score != c.want {
			t.Errorf("%s: score %v (ok %v), want %v", c.name, score, ok, c.want)
		}
	}
	p := Player{UCI: fake("mute.sh", `echo "bestmove e2e4"`)}
	if _, score, ok := p.pickScored(g, nil); !ok || !math.IsNaN(score) {
		t.Errorf("an engine that reports no score must yield NaN, got %v (ok %v)", score, ok)
	}
	for _, q := range []Player{{Random: true}, {Greedy: true}} {
		if _, score, ok := q.pickScored(g, nil); !ok || !math.IsNaN(score) {
			t.Errorf("a non-searching player must yield NaN, got %v (ok %v)", score, ok)
		}
	}
}

// NaN is "no opinion": it never counts toward a decision and it breaks a
// streak, so one unscored ply in a run of level ones starts the count over.
func TestUnknownScoreIsNotAnObservation(t *testing.T) {
	var a adjudicator
	for ply := 60; ply < 160; ply++ {
		if _, decided := a.observe(0, math.NaN(), ply); decided {
			t.Fatal("a NaN gap decided a game")
		}
		if _, _, drawn := a.observeDraw(math.NaN(), ply); drawn {
			t.Fatal("a NaN score drew a game")
		}
	}
	var b adjudicator
	drawn := false
	for i := 0; i < AdjudicateDrawPlies-1; i++ {
		_, _, drawn = b.observeDraw(0, 60+i)
	}
	_, _, drawn = b.observeDraw(math.NaN(), 60+AdjudicateDrawPlies-1)
	for i := 0; i < AdjudicateDrawPlies-1 && !drawn; i++ {
		_, _, drawn = b.observeDraw(0, 60+AdjudicateDrawPlies+i)
	}
	if drawn {
		t.Error("a NaN in the middle of a level streak did not break it")
	}
}
