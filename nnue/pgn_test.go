package main

import (
	"path/filepath"
	"strings"
	"testing"

	"chess/engine"
)

const operaGamePGN = `[Event "Paris"]
[Site "Paris FRA"]
[Date "1858.??.??"]
[White "Morphy, Paul"]
[Black "Duke Karl / Count Isouard"]
[Result "1-0"]

1.e4 e5 2.Nf3 d6 3.d4 Bg4 4.dxe5 Bxf3 5.Qxf3 dxe5 6.Bc4 Nf6 7.Qb3 Qe7
8.Nc3 c6 9.Bg5 b5 10.Nxb5 cxb5 11.Bxb5+ Nbd7 12.O-O-O Rd8 13.Rxd7 Rxd7
14.Rd1 Qe6 15.Bxd7+ Nxd7 16.Qb8+ Nxb8 17.Rd8# 1-0
`

// The same game with the annotations Lichess ships for analysed games.
const operaGameWithEvals = `[Event "Paris"]
[Site "Paris FRA"]
[White "Morphy, Paul"]
[Black "Duke Karl / Count Isouard"]
[Result "1-0"]

1. e4 { [%eval 0.17] [%clk 0:05:00] } 1... e5 { [%eval 0.24] }
2. Nf3 { [%eval 0.15] } 2... d6 { [%eval 0.72] } 3. d4 { [%eval 0.65] }
3... Bg4 { [%eval 1.02] } 4. dxe5 { [%eval 0.88] } 4... Bxf3 { [%eval 0.91] }
5. Qxf3 { [%eval 0.95] } 5... dxe5 { [%eval 1.10] } 6. Bc4 { [%eval 1.06] }
6... Nf6 { [%eval 1.44] } 7. Qb3 { [%eval 1.31] } 7... Qe7 { [%eval 1.62] }
8. Nc3 { [%eval 1.55] } 8... c6 { [%eval 1.83] } 9. Bg5 { [%eval 1.79] }
9... b5 { [%eval 3.21] } 10. Nxb5 { [%eval 3.10] } 10... cxb5 { [%eval 4.02] }
11. Bxb5+ { [%eval 3.98] } 11... Nbd7 { [%eval 4.55] }
12. O-O-O { [%eval 4.40] } 12... Rd8 { [%eval 5.12] }
13. Rxd7 { [%eval 5.01] } 13... Rxd7 { [%eval 6.30] }
14. Rd1 { [%eval 6.22] } 14... Qe6 { [%eval 9.99] }
15. Bxd7+ { [%eval 9.80] } 15... Nxd7 { [%eval 12.5] }
16. Qb8+ { [%eval 12.1] } 16... Nxb8 { [%eval 99.9] }
17. Rd8# { [%eval 99.9] } 1-0
`

// The rule this whole path exists to obey: nothing an external engine
// computed about a position may reach the training data. Lichess ships
// `[%eval ...]` inside movetext comments, so switching from the evaluation
// dump to a games dump is not by itself enough.
//
// The test is behavioural rather than a grep: import the same game with and
// without the annotations and require byte-identical training samples. If
// any score leaked into a label, the two pools differ.
func TestPGNImportIgnoresEmbeddedEvaluations(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.bin")
	annotated := filepath.Join(dir, "annotated.bin")

	for _, c := range []struct{ text, path string }{
		{operaGamePGN, plain},
		{operaGameWithEvals, annotated},
	} {
		if err := ImportPGN(strings.NewReader(c.text), c.path, 0, 2, 6, 0.8, 0.35, 0, false, engine.Strong(2)); err != nil {
			t.Fatal(err)
		}
	}

	a, err := loadPool(plain, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadPool(annotated, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) == 0 {
		t.Fatal("imported nothing from the unannotated game")
	}
	if len(a) != len(b) {
		t.Fatalf("annotated game produced %d samples, unannotated %d: the comments "+
			"changed what was imported", len(b), len(a))
	}
	for i := range a {
		if a[i].target != b[i].target || a[i].static != b[i].static {
			t.Fatalf("sample %d differs: target %v against %v. An external evaluation "+
				"reached the label", i, a[i].target, b[i].target)
		}
	}
}

func TestStripCommentsRemovesEvalsAndVariations(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"1. e4 { [%eval 0.17] } e5", "1. e4  e5"},
		{"1. e4 (1. d4 d5) e5", "1. e4  e5"},
		{"1. e4 { [%clk 0:05:00] [%eval -1.2] } e5 { x }", "1. e4  e5 "},
	} {
		if got := stripComments(tc.in); got != tc.want {
			t.Errorf("stripComments(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if strings.Contains(stripComments(operaGameWithEvals), "eval") {
		t.Error("an eval annotation survived stripping")
	}
}

func TestPGNResultIsTakenFromTheHeader(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		want float64
		ok   bool
	}{
		{"1-0", 1, true}, {"0-1", 0, true}, {"1/2-1/2", 0.5, true}, {"*", 0, false},
	} {
		got, ok := resultOf(tc.tag)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("resultOf(%q) = %v %v, want %v %v", tc.tag, got, ok, tc.want, tc.ok)
		}
	}
}

// A game whose movetext will not replay must be dropped whole, not
// half-imported: positions after the bad token were never reached.
func TestPGNSkipsGamesThatWillNotReplay(t *testing.T) {
	bad := `[Result "1-0"]

1.e4 e5 2.Nf3 Nc6 3.Bb5 a6 4.Qxz9 Nf6 5.O-O Be7 6.Re1 b5 1-0
`
	path := filepath.Join(t.TempDir(), "pool.bin")
	if err := ImportPGN(strings.NewReader(bad), path, 0, 2, 4, 0.8, 0.35, 0, false, engine.Strong(2)); err != nil {
		t.Fatal(err)
	}
	if got, _ := loadPool(path, 0); len(got) != 0 {
		t.Errorf("imported %d samples from a game that cannot be replayed", len(got))
	}
}

// A run interrupted after an hour must not restart from the first game and
// append everything a second time. The pool would silently gain duplicates
// and the compute would be paid twice.
func TestPGNImportResumesRatherThanRepeating(t *testing.T) {
	two := operaGamePGN + "\n" + strings.Replace(operaGamePGN, `[Site "Paris FRA"]`, `[Site "Paris FRA 2"]`, 1)
	path := filepath.Join(t.TempDir(), "pool.bin")

	// First pass takes one game's worth and stops.
	if err := ImportPGN(strings.NewReader(two), path, 1, 2, 6, 0.8, 0.35, 0, false, engine.Strong(2)); err != nil {
		t.Fatal(err)
	}
	first, _ := loadPool(path, 0)
	if len(first) == 0 {
		t.Fatal("first pass imported nothing")
	}

	// Second pass over the same input resumes instead of repeating.
	if err := ImportPGN(strings.NewReader(two), path, 0, 2, 6, 0.8, 0.35, 0, true, engine.Strong(2)); err != nil {
		t.Fatal(err)
	}
	all, _ := loadPool(path, 0)
	if len(all) >= 2*len(first)+len(first)/2 {
		t.Errorf("pool holds %d samples after resuming over 2 games, first pass held %d: "+
			"the resume re-imported", len(all), len(first))
	}
}

// The labeller must actually change the labels.
//
// A flag that is accepted and ignored is the most expensive kind of bug in
// this pipeline: the run looks healthy, the loss falls, and every
// measurement taken over the flag is noise. It has already happened twice
// here, with Adam and with weight decay, and the bootstrap ladder is built
// entirely on this flag doing something.
func TestPGNLabellerChangesTheLabels(t *testing.T) {
	dir := t.TempDir()
	shallow := filepath.Join(dir, "shallow.bin")
	deep := filepath.Join(dir, "deep.bin")

	if err := ImportPGN(strings.NewReader(operaGamePGN), shallow, 0, 1, 6,
		0.8, 0.35, 0, false, engine.Strong(1)); err != nil {
		t.Fatal(err)
	}
	if err := ImportPGN(strings.NewReader(operaGamePGN), deep, 0, 5, 6,
		0.8, 0.35, 0, false, engine.Strong(5)); err != nil {
		t.Fatal(err)
	}

	a, _ := loadPool(shallow, 0)
	b, _ := loadPool(deep, 0)
	if len(a) == 0 || len(b) == 0 {
		t.Fatalf("imported %d and %d positions", len(a), len(b))
	}
	// The two runs see the same positions, so any difference is the
	// labeller. Requiring only that some differ, not all: a quiet position
	// can genuinely score the same at both depths.
	differing := 0
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i].target != b[i].target {
			differing++
		}
	}
	if differing == 0 {
		t.Error("a depth-1 labeller and a depth-5 labeller produced identical targets " +
			"for every position: the labeller is not being used")
	}
	t.Logf("%d of %d targets differ between a depth-1 and a depth-5 labeller",
		differing, len(a))
}
