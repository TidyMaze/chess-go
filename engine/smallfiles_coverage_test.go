package engine

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"chess/board"
	"chess/game"
)

// The pre-HalfKP residual network still loads and evaluates.
func TestLegacyNetRoundTripsAndEvaluates(t *testing.T) {
	const hidden = 8 // the width is whatever B1 says it is
	n := &Net{W1: make([]float32, nnueInputs*hidden), B1: make([]float32, hidden),
		W2: make([]float32, hidden), B2: 0.25, Scale: 2, Residual: true}
	for i := range n.W2 {
		n.W2[i] = 0.01
	}
	g := game.New()
	if v := n.Evaluate(&g.Board); math.IsNaN(v) {
		t.Error("NaN evaluation")
	}
	path := filepath.Join(t.TempDir(), "net.json")
	if err := n.Save(path); err != nil {
		t.Fatal(err)
	}
	m, err := LoadNet(path)
	if err != nil || m.B2 != n.B2 || !m.Residual {
		t.Fatalf("round trip: %+v %v", m, err)
	}
	if _, err := LoadNet(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("loading a missing file did not fail")
	}
	if featureIndex(board.Black, board.Queen, board.Sq{File: 7, Rank: 7}) == featureIndex(board.White, board.Queen, board.Sq{File: 7, Rank: 7}) {
		t.Error("colours share a feature index")
	}
	if s := Sigmoid(0, 0.3); math.Abs(s-0.5) > 1e-12 {
		t.Errorf("sigmoid(0) = %v", s)
	}
}

func TestRatingFitsFromPairScores(t *testing.T) {
	// Three players: 0 beats 1 at 75%, 1 beats 2 at 75%, 0 anchored at 0.
	r := FitFromResults(3, []PairScore{{0, 1, 0.75, 100}, {1, 2, 0.75, 100}}, 0)
	if len(r) != 3 || r[0] != 0 || !(r[1] < r[0]) || !(r[2] < r[1]) {
		t.Errorf("ratings %v: expected a descending ladder anchored at 0", r)
	}
	// From actual games, tiny.
	players := []Player{{Random: true, Name: "r1"}, {Random: true, Name: "r2"}}
	got := FitRatings(players, 2, 20, 0)
	if len(got) != 2 || got[0] != 0 {
		t.Errorf("FitRatings %v", got)
	}
}

func TestSPRTLifecycle(t *testing.T) {
	s := NewSPRT(0, 20)
	if s.Status() != SPRTContinue || s.Games() != 0 {
		t.Errorf("fresh test: %v %d", s.Status(), s.Games())
	}
	lo, hi := s.Bounds()
	if !(lo < 0 && hi > 0) {
		t.Errorf("bounds %v %v", lo, hi)
	}
	s.Add(300, 100, 50)
	if s.Status() != SPRTAcceptH1 || s.Elo() <= 0 || s.LLR() <= 0 {
		t.Errorf("a dominant result: %v elo %.0f llr %.2f", s.Status(), s.Elo(), s.LLR())
	}
	f := NewSPRT(0, 20)
	f.Add(50, 100, 300)
	if f.Status() != SPRTAcceptH0 {
		t.Errorf("a losing result: %v", f.Status())
	}
	for _, r := range []SPRTResult{SPRTContinue, SPRTAcceptH1, SPRTAcceptH0, SPRTResult(9)} {
		if r.String() == "" {
			t.Errorf("empty string for %d", r)
		}
	}
	// Degenerate: all draws, or no games, must not be NaN.
	d := NewSPRT(0, 20)
	d.Add(0, 10, 0)
	if math.IsNaN(d.LLR()) || math.IsNaN(d.Elo()) {
		t.Error("NaN on all draws")
	}
}

func TestLadderMeasuresAndPrints(t *testing.T) {
	rungs := MeasureLadder([]Player{{Random: true, Name: "a"}, {Greedy: true, Name: "b"}, {Depth: 1, Name: "c"}}, 2, 20)
	if len(rungs) != 3 {
		t.Fatalf("%d rungs", len(rungs))
	}
	for _, r := range rungs {
		if r.String() == "" {
			t.Error("empty rung string")
		}
	}
}

func TestOpeningsListLoadsAndFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openings.txt")
	os.WriteFile(path, []byte("rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1\n\n# comment\nnot a fen\n"), 0o600)
	list, err := LoadOpeningsList(path)
	if err != nil || len(list) == 0 {
		t.Fatalf("list %v err %v", list, err)
	}
	if _, err := LoadOpeningsList(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("a missing file loaded")
	}
}

func TestChampionDefaultsAndSilentFallback(t *testing.T) {
	d := DefaultChampion()
	if d.Depth <= 0 {
		t.Errorf("default depth %d", d.Depth)
	}
	c := Champion{Depth: 3, NetFile: filepath.Join(t.TempDir(), "absent.json")}
	p := c.Player() // falls back to the hand evaluation with a warning
	if p.HalfKP != nil || p.Depth != 3 {
		t.Errorf("fallback player %+v", p)
	}
	w := NewChampionWatcher(filepath.Join(t.TempDir(), "nothing.json"))
	if _, pl := w.Current(); pl.Depth <= 0 {
		t.Error("watcher on a missing file must serve the default champion")
	}
	if err := WriteChampion(filepath.Join(t.TempDir(), "no", "dir", "c.json"), d); err == nil {
		t.Error("writing into a missing directory succeeded")
	}
}

func TestEvaluationExtrasAndShapeTermsRun(t *testing.T) {
	g := mustFEN(t, "r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	base := Strong(1)
	ex := base
	ex.Extras = true
	sh := base
	sh.Shape = true
	w := DefaultShapeWeights()
	sh.ShapeW = &w
	a := PlayerStaticEval(base, &g.Board)
	if b := PlayerStaticEval(ex, &g.Board); b == a {
		t.Log("extras changed nothing on this position (allowed), but the code ran")
	}
	_ = PlayerStaticEval(sh, &g.Board)
	_ = DefaultMobilityWeights()
	_ = DefaultStructureWeights()
	_ = DefaultPSTScale()
	SeedRandom(1)
}

func TestHalfKPCanonicalKingAndProbabilityOutput(t *testing.T) {
	if kingCanonicalSquare(Sq{File: 6, Rank: 0}) != kingCanonicalSquare(Sq{File: 1, Rank: 0}) {
		t.Error("mirrored king files must share a canonical square")
	}
	if kingCanonicalSquare(Sq{File: 0, Rank: 7}) != 28 {
		t.Errorf("a8 canonical %d, want 28", kingCanonicalSquare(Sq{File: 0, Rank: 7}))
	}
	if v := probabilityToPawns(0.5, 0.3); math.Abs(v) > 1e-9 {
		t.Errorf("p=0.5 -> %v pawns", v)
	}
	if v := probabilityToPawns(1, 0.3); v != 12 {
		t.Errorf("certainty clamps to 12 pawns, got %v", v)
	}
	if v := probabilityToPawns(0, 0.3); v != -12 {
		t.Errorf("certain loss clamps to -12 pawns, got %v", v)
	}
}
