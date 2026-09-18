package main

import (
	"strings"
	"testing"

	"chess/game"
)

func TestPhaseSplitsByMaterialLeft(t *testing.T) {
	cases := map[string]string{
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1": "opening",
		"r1bq1rk1/pp3ppp/2n1pn2/3p4/3P4/2N1PN2/PP3PPP/R1BQ1RK1 w - - 0 9": "middlegame",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1":                      "endgame",
		"8/8/4k3/8/8/8/8/K6R w - - 0 1":                                  "endgame",
	}
	for fen, want := range cases {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatalf("ParseFEN(%q): %v", fen, err)
		}
		if got := phase(g); got != want {
			t.Errorf("phase(%q) = %q, want %q", fen, got, want)
		}
	}
}

// The worst disagreements are the point of the audit: a list of
// positions to look at. Keeping the first ones seen instead of the
// costliest would fill it with opening moves nobody cares about.
func TestTallyKeepsTheCostliestDisagreements(t *testing.T) {
	tl := newTally(2)
	tl.add(disagreement{phase: "opening", costPawn: 0.1, fen: "cheap"}, false)
	tl.add(disagreement{phase: "middlegame", costPawn: 3.5, fen: "dear"}, false)
	tl.add(disagreement{phase: "endgame", costPawn: 1.2, fen: "middling"}, false)
	tl.add(disagreement{phase: "endgame", costPawn: 0.2, fen: "agreed"}, true)
	if len(tl.worst) != 2 {
		t.Fatalf("kept %d disagreements, asked for 2", len(tl.worst))
	}
	if tl.worst[0].fen != "dear" || tl.worst[1].fen != "middling" {
		t.Errorf("kept %q and %q, want the two costliest", tl.worst[0].fen, tl.worst[1].fen)
	}
	if tl.seen["endgame"] != 2 || tl.agreed["endgame"] != 1 {
		t.Errorf("endgame seen %d agreed %d, want 2 and 1", tl.seen["endgame"], tl.agreed["endgame"])
	}
}

func TestReportShowsEveryPhaseAndATotal(t *testing.T) {
	tl := newTally(1)
	tl.add(disagreement{phase: "opening"}, true)
	tl.add(disagreement{phase: "opening"}, false)
	got := tl.report()
	for _, want := range []string{"opening", "middlegame", "endgame", "all", "50.0%"} {
		if !strings.Contains(got, want) {
			t.Errorf("report is missing %q:\n%s", want, got)
		}
	}
}
