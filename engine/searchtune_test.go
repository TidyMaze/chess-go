package engine

import (
	"math"
	"testing"

	"chess/game"
)

// A default tune (nil, or DefaultSearchTune's own values) must not move a
// single node count: this is the golden BenchmarkChampionSearch's real
// job, and this test is the same claim on the ordinary position set.
func TestSearchTuneNilMatchesNoOverride(t *testing.T) {
	var without, with int
	for _, fen := range correctnessPositions[:6] {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatalf("ParseFEN(%q): %v", fen, err)
		}
		a := Strong(6)
		a.ScaledLMR, a.LMP, a.DeepRFP, a.Razoring, a.NullGate = true, true, true, true, true
		b := a
		def := DefaultSearchTune()
		b.Tune = &def
		ResetNodes()
		PlayerScoreWith(a, g, nil)
		without += TotalNodes()
		ResetNodes()
		PlayerScoreWith(b, g, nil)
		with += TotalNodes()
	}
	if without != with {
		t.Errorf("DefaultSearchTune changed node count: %d without a Tune, %d with the explicit default", without, with)
	}
}

// A non-default tune has to actually reach the search: change LMRDiv and
// the node count moves, through Player.Tune alone.
func TestSearchTuneChangesNodeCount(t *testing.T) {
	var base, tuned int
	for _, fen := range correctnessPositions[:6] {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatalf("ParseFEN(%q): %v", fen, err)
		}
		a := Strong(6)
		a.ScaledLMR = true
		b := a
		tune := DefaultSearchTune()
		tune.LMRDiv = 1.2
		b.Tune = &tune
		ResetNodes()
		PlayerScoreWith(a, g, nil)
		base += TotalNodes()
		ResetNodes()
		PlayerScoreWith(b, g, nil)
		tuned += TotalNodes()
	}
	t.Logf("nodes: %d base LMRDiv, %d LMRDiv=1.2", base, tuned)
	if base == tuned {
		t.Error("changing LMRDiv through Player.Tune did not move the node count")
	}
}

// The LMR table a SearchTune builds must read exactly like the package's
// precomputed default table when the tune carries the default constants,
// for every depth and move index the search can reach.
func TestSearchTuneLMRTableMatchesDefault(t *testing.T) {
	def := DefaultSearchTune()
	c := &searchCtx{ev: &Eval{Tune: &def}}
	for d := 1; d < 64; d++ {
		for m := 0; m < 64; m++ {
			got := c.lmrValue(d, m)
			want := lmrTable(d, m)
			if got != want {
				t.Fatalf("depth %d move %d: tuned table %d, default table %d", d, m, got, want)
			}
		}
	}
}

// nullMoveReduction's two truncations, base-plus-depth-share then the
// margin bonus, must produce the exact same plies through SearchTune at
// the default constants, for every depth the search reaches and several
// margins either side of the cap.
func TestSearchTuneNullReductionMatchesDefault(t *testing.T) {
	var tune *SearchTune
	def := DefaultSearchTune()
	for _, seed := range []*SearchTune{nil, &def} {
		tune = seed
		for depth := 0; depth <= 64; depth++ {
			for _, margin := range []float64{0, 0.5, 1.4999, 1.5, 3.0, 9.0} {
				got := tune.reductionNull(depth, margin)
				want := nullMoveReduction(depth, margin)
				if got != want {
					t.Fatalf("nil-tune=%v depth %d margin %.4f: tuned %d, default %d", seed == nil, depth, margin, got, want)
				}
			}
		}
	}
}

// lateMovePrunedMax's move-count threshold must match exactly through
// SearchTune at the default LMPBase, for every depth 0..64 and both
// improving states, at the default and deep move caps.
func TestSearchTuneLMPMatchesDefault(t *testing.T) {
	def := DefaultSearchTune()
	for _, tune := range []*SearchTune{nil, &def} {
		for depth := 0; depth <= 64; depth++ {
			for _, improving := range []bool{false, true} {
				for _, maxDepth := range []int{5, 8} {
					for _, moveIndex := range []int{0, 1, 5, 12, 20, 40} {
						got := tune.prunedLMP(depth, moveIndex, improving, false, false, false, false, false, maxDepth)
						want := lateMovePrunedMax(depth, moveIndex, improving, false, false, false, false, false, maxDepth)
						if got != want {
							t.Fatalf("depth %d move %d improving %v maxDepth %d: tuned %v, default %v",
								depth, moveIndex, improving, maxDepth, got, want)
						}
					}
				}
			}
		}
	}
}

// ParseSearchTune and String must round-trip: every field String prints
// is a field ParseSearchTune understands, and parsing what it printed
// reproduces the same values.
func TestParseSearchTuneRoundTrip(t *testing.T) {
	def := DefaultSearchTune()
	def.LMRDiv = 1.75
	def.NullBase = 4
	s := def.String()
	got, err := ParseSearchTune(s)
	if err != nil {
		t.Fatalf("ParseSearchTune(%q): %v", s, err)
	}
	if got == nil {
		t.Fatal("ParseSearchTune of a non-empty string returned nil")
	}
	if *got != def {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", *got, def)
	}
}

// An override string only touches the named fields; everything else
// stays at its default.
func TestParseSearchTuneOverridesOnDefaults(t *testing.T) {
	got, err := ParseSearchTune("RFPBase=0.6,LMRDiv=2.3")
	if err != nil {
		t.Fatalf("ParseSearchTune: %v", err)
	}
	want := DefaultSearchTune()
	want.RFPBase = 0.6
	want.LMRDiv = 2.3
	if *got != want {
		t.Errorf("got %+v, want %+v", *got, want)
	}
}

func TestParseSearchTuneEmptyIsNil(t *testing.T) {
	got, err := ParseSearchTune("")
	if err != nil {
		t.Fatalf("ParseSearchTune(\"\"): %v", err)
	}
	if got != nil {
		t.Errorf("ParseSearchTune(\"\") = %+v, want nil", got)
	}
}

func TestParseSearchTuneUnknownFieldErrors(t *testing.T) {
	if _, err := ParseSearchTune("NotAField=1"); err == nil {
		t.Error("an unknown field name did not error")
	}
	if _, err := ParseSearchTune("RFPBase"); err == nil {
		t.Error("a value with no '=' did not error")
	}
	if _, err := ParseSearchTune("RFPBase=notanumber"); err == nil {
		t.Error("an unparseable value did not error")
	}
}

// Sanity check on the formulas themselves, independent of the defaults:
// a tuned RFP/razor margin still grows with depth the way the untuned
// one does.
func TestSearchTuneMarginsStillGrowWithDepth(t *testing.T) {
	tune := DefaultSearchTune()
	tune.RFPSlope = 0.7
	tune.RazorSlope = 3.0
	prevRFP, prevRazor := math.Inf(-1), math.Inf(-1)
	for d := 1; d <= 7; d++ {
		if m := tune.marginRFP(d); m <= prevRFP {
			t.Errorf("tuned RFP margin did not grow at depth %d", d)
		} else {
			prevRFP = m
		}
		if d <= 3 {
			if m := tune.marginRazor(d); m <= prevRazor {
				t.Errorf("tuned razor margin did not grow at depth %d", d)
			} else {
				prevRazor = m
			}
		}
	}
}
