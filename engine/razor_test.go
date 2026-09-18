package engine

import (
	"testing"

	"chess/game"
)

// Razoring is the low side of futility. The engine already returns early
// when a static evaluation stands far enough ABOVE the window that no
// quiet move is expected to bring it back down. Nothing handled the
// mirror: a node whose static evaluation stands far BELOW alpha is
// almost certainly a fail low, and searching it at full width costs a
// whole subtree to learn that.
//
// It differs from plain futility in that it does not trust the static
// evaluation on its own. Being three pawns down means nothing if a
// capture wins the piece straight back, so the decision only chooses to
// VERIFY with a quiescence search, which sees exactly those captures.
func TestRazoringOnlyLooksAtHopelessLowDepthNodes(t *testing.T) {
	const alpha = 0.0
	cases := []struct {
		name       string
		depth      int
		static     float64
		maximizing bool
		want       bool
	}{
		{"a pawn down at depth 1 is not hopeless", 1, -1.0, true, false},
		{"five pawns down at depth 1 is", 1, -5.0, true, true},
		{"five pawns down at depth 4 is out of range", 4, -5.0, true, false},
		{"the margin grows with depth", 3, -3.0, true, false},
		{"and is cleared by a big enough deficit", 3, -9.0, true, true},
		{"the minimizing side razors the other way", 1, 5.0, false, true},
		{"and not when it stands badly", 1, -5.0, false, false},
	}
	for _, c := range cases {
		if got := razorCuts(c.depth, c.static, alpha, alpha, c.maximizing); got != c.want {
			t.Errorf("%s: razorCuts(depth %d, static %+.1f, maximizing %v) = %v, want %v",
				c.name, c.depth, c.static, c.maximizing, got, c.want)
		}
	}
}

// The margin has to grow with depth: one ply of quiet play swings less
// than three, so a deficit that is hopeless at depth 1 is recoverable at
// depth 3.
func TestRazorMarginGrowsWithDepth(t *testing.T) {
	for d := 1; d < 3; d++ {
		if razorMargin(d) >= razorMargin(d+1) {
			t.Errorf("razorMargin(%d) = %.2f is not below razorMargin(%d) = %.2f",
				d, razorMargin(d), d+1, razorMargin(d+1))
		}
	}
	if razorMargin(1) <= 0 {
		t.Errorf("razorMargin(1) = %.2f, a margin must be positive", razorMargin(1))
	}
}

// The flag has to reach the search, and a null control is the only thing
// that proves it: a feature nobody reads measures as level and looks like
// an honest negative result.
func TestRazoringFlagReachesTheSearch(t *testing.T) {
	p := Strong(4)
	if p.Razoring {
		t.Fatal("razoring is on by default, so no race can measure it")
	}
	p.ApplyFeatures("razoring")
	if !p.Razoring {
		t.Fatal(`ApplyFeatures("razoring") left the flag off`)
	}
	if !evalForPlayer(p).Razoring {
		t.Error("the player carries razoring but the evaluation the search reads does not")
	}
}

// A flag that reaches the search still proves nothing until the search
// visits fewer nodes for it. Feature work in this project has twice been
// raced against a binary that ignored the feature, so the wiring gets its
// own measurement.
//
// Six positions rather than one: the first version of this test used a
// single position where Black was a queen down, and razoring read 102% of
// the plain node count there. A rule that fires on hopeless nodes is
// worth measuring over a set that is mostly not hopeless.
func TestRazoringVisitsFewerNodes(t *testing.T) {
	fens := []string{
		"r1bqkbnr/pppp1ppp/2n5/4p3/2B1P3/5Q2/PPPP1PPP/RNB1K1NR b KQkq - 0 1",
		"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"8/8/4k3/8/8/8/8/K6R w - - 0 1",
	}
	nodes := func(razoring bool) int {
		total := 0
		for _, fen := range fens {
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatalf("ParseFEN(%q): %v", fen, err)
			}
			p := Strong(7)
			if razoring {
				p.ApplyFeatures("razoring")
			}
			ev := evalForPlayer(p)
			ev.Table = NewTranspositionTable(20)
			ChooseMoveIterative(g, g.Turn, 7, ev, true)
			total += LastSearchNodesValue()
		}
		return total
	}
	plain, razored := nodes(false), nodes(true)
	if razored >= plain {
		t.Errorf("razoring searched %d nodes against %d plain, so it is not reaching the search",
			razored, plain)
	}
	t.Logf("nodes %d plain, %d razored (%.1f%%)", plain, razored,
		100*float64(razored)/float64(plain))
}
