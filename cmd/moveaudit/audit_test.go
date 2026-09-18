package main

import (
	"math"
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

// A position already won by five pawns tells us nothing: nearly every
// sane move keeps the win, the judge picks the fastest mate, and the
// disagreement that comes back is a preference, not a defect. Worse, a
// mate score is reported as 100 pawns, so one of those in the cost
// arithmetic produced an "87 pawn blunder" that was really "we kept a
// winning position instead of mating".
func TestDecidedPositionsAreNotWorthJudging(t *testing.T) {
	for _, c := range []struct {
		name  string
		score float64
		want  bool
	}{
		{"level", 0.2, true},
		{"an edge", 2.0, true},
		{"nearly won", 4.9, true},
		{"won", 6.0, false},
		{"lost", -6.0, false},
		{"mate", 100, false},
		{"mated", -100, false},
	} {
		if got := worthJudging(c.score, 5.0); got != c.want {
			t.Errorf("%s (%.1f): worthJudging = %v, want %v", c.name, c.score, got, c.want)
		}
	}
}

// The costliest list is for reading; the full list is for counting. A
// theme judged on twenty-five rows is a coincidence with a story.
func TestTallyKeepsEveryDisagreementForCounting(t *testing.T) {
	tl := newTally(1)
	for i := 0; i < 5; i++ {
		tl.add(disagreement{phase: "middlegame", costPawn: float64(i)}, false)
	}
	tl.add(disagreement{phase: "middlegame"}, true)
	if len(tl.worst) != 1 {
		t.Errorf("printed list holds %d, asked for 1", len(tl.worst))
	}
	if len(tl.all) != 5 {
		t.Errorf("full list holds %d disagreements, want 5", len(tl.all))
	}
}

// What our move costs is only measurable against the judge's own move
// searched the same way. Scoring the parent and then the child compares
// two different horizons: the child gets a full depth of its own, so the
// side to move alternates and the comparison carries a systematic bias.
// Measured over 384 disagreements, that bias averaged -0.67 pawns, which
// said our moves were better than the judge's best.
func TestCostComparesTwoChildrenNotParentAndChild(t *testing.T) {
	// Both scores are from the opponent's point of view, since it is the
	// opponent to move after either candidate. Higher is better for them.
	cases := []struct {
		name              string
		ourChild, theirs  float64
		want              float64
	}{
		{"our move gives the opponent more", 0.8, 0.1, 0.7},
		{"the moves are equally good", 0.3, 0.3, 0},
		{"our move is better than the judge's", -0.2, 0.4, -0.6},
	}
	for _, c := range cases {
		if got := costOfOurMove(c.ourChild, c.theirs); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: costOfOurMove(%.1f, %.1f) = %.2f, want %.2f",
				c.name, c.ourChild, c.theirs, got, c.want)
		}
	}
}

func TestScoreErrorSeparatesBiasFromNoise(t *testing.T) {
	// Optimistic every time: the mean and the mean absolute agree.
	var biased scoreError
	for _, d := range []float64{1.0, 1.0, 1.0} {
		biased.add(d, 0)
	}
	if got := biased.report(); !strings.Contains(got, "mean +1.00") || !strings.Contains(got, "absolute 1.00") {
		t.Errorf("a consistent overestimate must show as bias:\n%s", got)
	}
	// Wrong by the same amount either way: no bias, but the error is real.
	var noisy scoreError
	noisy.add(1.0, 0)
	noisy.add(-1.0, 0)
	if got := noisy.report(); !strings.Contains(got, "mean +0.00") || !strings.Contains(got, "absolute 1.00") {
		t.Errorf("symmetric error must show as noise, not bias:\n%s", got)
	}
}
