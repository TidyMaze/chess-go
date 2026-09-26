package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"chess/engine"
	"chess/game"
)

// Game records spell a queen promotion "e7e8q". When the champion queens
// again at both budgets, the probe must see the same move, not a fix.
func TestProbeWorstMatchesARecordedQueenPromotion(t *testing.T) {
	fen := "8/4P1k1/8/8/8/8/6K1/8 w - - 0 1"
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := game.MoveFromUCI("e7e8")
	recorded := g.MoveUCI(m)
	worst := []result{{r: engine.GameRecord{}, w: worstDrop{found: true, ply: 1, drop: 5,
		at: plyEval{move: recorded, before: fen}}}}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	probeWorst("../../champion.json", worst, 1)
	w.Close()
	os.Stdout = stdout
	out, _ := io.ReadAll(r)

	if !strings.Contains(string(out), "same move: blind spot") {
		t.Errorf("the champion queens at both budgets, as recorded (%q), yet the probe says otherwise:\n%s", recorded, out)
	}
}
