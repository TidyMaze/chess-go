package main

import (
	"chess/board"
	"chess/game"
	"fmt"
	"math"
	"os"
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

// Does the label depth argument alone change the labels?
//
// TestPGNLabellerChangesTheLabels varies the depth and the labeller
// together, so it stays green if the depth argument is ignored and only
// the player matters. This holds the player fixed and varies nothing but
// the depth, which is the flag the label-depth experiment turns.
//
// It is the same shape as the bug that was already found here once: a
// -label-champion flag that was read, stored, and never used, so every
// rung of the ladder produced identical labels and nothing in the numbers
// said so.
func TestPGNLabelDepthAloneChangesTheLabels(t *testing.T) {
	dir := t.TempDir()
	shallow := filepath.Join(dir, "shallow.bin")
	deep := filepath.Join(dir, "deep.bin")

	// One labeller, used for both runs. Its own depth is deliberately a
	// third value, so a run that ignored the argument would produce two
	// identical pools rather than accidentally matching one of them.
	labeller := engine.Strong(3)
	if err := ImportPGN(strings.NewReader(operaGamePGN), shallow, 0, 1, 6,
		0.8, 0.35, 0, false, labeller); err != nil {
		t.Fatal(err)
	}
	if err := ImportPGN(strings.NewReader(operaGamePGN), deep, 0, 6, 6,
		0.8, 0.35, 0, false, labeller); err != nil {
		t.Fatal(err)
	}

	a, _ := loadPool(shallow, 0)
	b, _ := loadPool(deep, 0)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("the two runs must see the same positions: %d and %d", len(a), len(b))
	}
	differing := 0
	for i := range a {
		if a[i].target != b[i].target {
			differing++
		}
	}
	t.Logf("%d of %d labels differ between depth 1 and depth 6", differing, len(a))
	if differing == 0 {
		t.Error("labelling the same positions at depth 1 and depth 6 produced identical " +
			"targets, so the depth argument is not reaching the search")
	}
}

// The arms of a label-depth experiment have to be paired: same positions,
// different targets. If the depth changed which positions were kept, a
// difference in Elo could be the sample rather than the labels.
func TestPGNLabelDepthKeepsTheSamePositions(t *testing.T) {
	dir := t.TempDir()
	shallow := filepath.Join(dir, "a.bin")
	deep := filepath.Join(dir, "b.bin")
	labeller := engine.Strong(3)
	if err := ImportPGN(strings.NewReader(operaGamePGN), shallow, 0, 1, 6,
		0.8, 0.35, 0, false, labeller); err != nil {
		t.Fatal(err)
	}
	if err := ImportPGN(strings.NewReader(operaGamePGN), deep, 0, 6, 6,
		0.8, 0.35, 0, false, labeller); err != nil {
		t.Fatal(err)
	}
	a, _ := loadPool(shallow, 0)
	b, _ := loadPool(deep, 0)
	if len(a) != len(b) {
		t.Fatalf("different position counts, %d and %d: the arms are not paired",
			len(a), len(b))
	}
	for i := range a {
		if len(a[i].own) != len(b[i].own) || a[i].static != b[i].static {
			t.Fatalf("position %d differs between the two runs, so the label depth is "+
				"changing which positions are kept and not only how they are scored", i)
		}
	}
}

// A book built from the games people actually played, not from engine
// evaluations. The Opera Game opens 1.e4 e5 2.Nf3 d6, so a book built
// from it alone must answer e2e4 at the start and g1f3 after 1.e4 e5.
func TestBookFromPGNRecordsTheMovesPlayed(t *testing.T) {
	var out strings.Builder
	n, err := BuildBookFromPGN(strings.NewReader(operaGamePGN), &out, 6, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no positions written")
	}
	path := filepath.Join(t.TempDir(), "book.txt")
	if err := os.WriteFile(path, []byte(out.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	book, err := engine.LoadBook(path)
	if err != nil {
		t.Fatal(err)
	}
	g := game.New()
	m, ok := book.Move(g)
	if !ok || m.From != (board.Sq{File: 4, Rank: 1}) || m.To != (board.Sq{File: 4, Rank: 3}) {
		t.Fatalf("book at the start: %v %v, want e2e4", m, ok)
	}
	g.ApplyMove(m.From, m.To)
	e5, _ := game.MoveFromSAN(g, "e5")
	g.ApplyMove(e5.From, e5.To)
	m, ok = book.Move(g)
	if !ok || m.From != (board.Sq{File: 6, Rank: 0}) || m.To != (board.Sq{File: 5, Rank: 2}) {
		t.Fatalf("book after 1.e4 e5: %v %v, want g1f3", m, ok)
	}
	// Past the ply cap nothing is recorded.
	if n > 6 {
		t.Errorf("%d positions from a 6-ply cap on one game", n)
	}
}

// The game outcome has to reach the label, and it has to reach it as the
// game's actual result.
//
// This is the one lever that can lift a rung above its teacher: a network
// fitted only to the teacher's search score cannot pass the teacher, while
// the result of the game says what happened past the search's horizon.
// So if the lambda argument were quietly ignored the whole ladder would go
// on measuring pure distillation, which is the failure that has already
// cost this project seven rungs.
//
// The check is exact and needs no assumption about which side the target
// is written from. With lambda 1 the target is the search score alone, so
// for any other lambda
//
//	target(l) = l*target(1) + (1-l)*resultPawns
//
// and solving for resultPawns must give one of the three values a chess
// game can end in.
func TestPGNGameOutcomeReachesTheLabel(t *testing.T) {
	dir := t.TempDir()
	pure := filepath.Join(dir, "pure.bin")
	mixed := filepath.Join(dir, "mixed.bin")
	labeller := engine.Strong(3)
	const lambda = 0.2
	if err := ImportPGN(strings.NewReader(operaGamePGN), pure, 0, 3, 6,
		1.0, 0.35, 0, false, labeller); err != nil {
		t.Fatal(err)
	}
	if err := ImportPGN(strings.NewReader(operaGamePGN), mixed, 0, 3, 6,
		lambda, 0.35, 0, false, labeller); err != nil {
		t.Fatal(err)
	}

	a, _ := loadPool(pure, 0)
	b, _ := loadPool(mixed, 0)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("the two runs must see the same positions: %d and %d", len(a), len(b))
	}
	differing, checked := 0, 0
	for i := range a {
		if a[i].static != b[i].static {
			t.Fatalf("position %d differs between the runs, so lambda is changing which "+
				"positions are kept and not only how they are labelled", i)
		}
		if a[i].target != b[i].target {
			differing++
		}
		// A clamped target has lost the information this identity needs.
		if a[i].target >= 12 || a[i].target <= -12 ||
			b[i].target >= 12 || b[i].target <= -12 {
			continue
		}
		checked++
		result := (float64(b[i].target) - lambda*float64(a[i].target)) / (1 - lambda)
		if math.Abs(result-4) > 0.01 && math.Abs(result+4) > 0.01 && math.Abs(result) > 0.01 {
			t.Fatalf("position %d: target %.4f at lambda 1 and %.4f at lambda %.1f imply an "+
				"outcome term of %.4f pawns, which is not a win, a loss or a draw",
				i, a[i].target, b[i].target, lambda, result)
		}
	}
	t.Logf("%d of %d labels moved with lambda; %d checked against the game result",
		differing, len(a), checked)
	if differing == 0 {
		t.Error("labelling the same positions at lambda 1 and lambda 0.2 produced identical " +
			"targets, so the game outcome never reaches the label and every rung is pure " +
			"distillation")
	}
	if checked == 0 {
		t.Error("every target was clamped, so nothing was verified against the game result")
	}
}

// Games below the rating floor must not reach the book at all. The first
// book had no floor, and judged by this engine 8.5% of its moves lost more
// than half a pawn, the worst hanging a queen because twenty weak players
// had grabbed a bishop.
func TestBookFromPGNIgnoresGamesBelowTheRatingFloor(t *testing.T) {
	const weak = `[Event "weak"]
[White "a"]
[Black "b"]
[WhiteElo "1200"]
[BlackElo "1150"]
[Result "1-0"]

1. h4 h5 2. a4 a5 3. Rh3 Rh6 4. Ra3 Ra6 5. Rg3 Rg6 1-0
`
	var out strings.Builder
	n, err := BuildBookFromPGN(strings.NewReader(weak), &out, 6, 1, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("wrote %d positions from a game below the floor, want none:\n%s", n, out.String())
	}
	// The same game with no floor is kept, so the exclusion is the rating
	// and not something else about the game.
	out.Reset()
	n, err = BuildBookFromPGN(strings.NewReader(weak), &out, 6, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("no floor wrote nothing either, so the rating filter is not what excluded it")
	}
}

// A move that lost every time must not win its position just by being the
// most played one. This is the whole difference between a book of what was
// popular and a book of what worked.
func TestBookFromPGNPrefersTheMoveThatScored(t *testing.T) {
	// Three games play 1.a3 and lose, one plays 1.d4 and wins. Popularity
	// alone would answer a3.
	pgn := ""
	for i := 0; i < 3; i++ {
		pgn += `[Event "x"]
[WhiteElo "2400"]
[BlackElo "2400"]
[Result "0-1"]

1. a3 e5 2. b3 d5 3. c3 Nf6 4. d3 Nc6 5. e3 Bd6 0-1

`
	}
	pgn += `[Event "x"]
[WhiteElo "2400"]
[BlackElo "2400"]
[Result "1-0"]

1. d4 e5 2. b3 d5 3. c3 Nf6 4. d3 Nc6 5. e3 Bd6 1-0
`
	var out strings.Builder
	if _, err := BuildBookFromPGN(strings.NewReader(pgn), &out, 2, 1, 2000); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "book.txt")
	if err := os.WriteFile(path, []byte(out.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	book, err := engine.LoadBook(path)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := book.Move(game.New())
	if !ok {
		t.Fatal("the book has no move for the start position")
	}
	if got := m.UCI(); got != "d2d4" {
		t.Errorf("book answers %s at the start, want d2d4: a3 was played three times and lost every one of them", got)
	}
}

// A rare move with a lucky record must not beat a main line.
//
// This is the regression that put g8f6 into the book after 1.e4 Nc6 2.d4,
// where the previous book had d7d5. Stockfish rates the position before the
// move at -44 and after it at -165, so the book move gives away 1.2 pawns
// on move two of every game that reaches it, and it lost game U8eSZyvP.
//
// The cause is shrinkage that is too weak for the sample sizes involved: a
// move played a dozen times at 75% beat one played hundreds of times at
// 45%, because twenty games of prior barely moves a twelve game estimate.
func TestBookIgnoresALuckyRareMoveAgainstAMainLine(t *testing.T) {
	var pgn strings.Builder
	// The main line: played often, scoring the 45% a slightly worse
	// opening actually scores.
	for i := 0; i < 200; i++ {
		// Black, the side to move here, scores 45%: it wins 9 of every 20.
		result := "1-0"
		if i%20 < 9 {
			result = "0-1"
		}
		fmt.Fprintf(&pgn, "[Event \"x\"]\n[WhiteElo \"2400\"]\n[BlackElo \"2400\"]\n[Result \"%s\"]\n\n1. e4 e5 2. Nf3 Nc6 3. Bb5 a6 4. Ba4 Nf6 5. O-O Be7 6. Re1 b5 %s\n\n", result, result)
	}
	// The rare move: a dozen games, every one of them won.
	for i := 0; i < 12; i++ {
		// And Black wins every one of the twelve games with the rare move.
		fmt.Fprintf(&pgn, "[Event \"x\"]\n[WhiteElo \"2400\"]\n[BlackElo \"2400\"]\n[Result \"0-1\"]\n\n1. e4 e5 2. Nf3 Nc6 3. Bb5 a6 4. Ba4 b5 5. Bb3 Na5 6. O-O d6 0-1\n\n")
	}
	var out strings.Builder
	if _, err := BuildBookFromPGN(strings.NewReader(pgn.String()), &out, 8, 10, 2000); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "book.txt")
	if err := os.WriteFile(path, []byte(out.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	book, err := engine.LoadBook(path)
	if err != nil {
		t.Fatal(err)
	}
	g := game.New()
	for _, san := range []string{"e4", "e5", "Nf3", "Nc6", "Bb5", "a6", "Ba4"} {
		m, ok := game.MoveFromSAN(g, san)
		if !ok {
			t.Fatalf("%s did not parse", san)
		}
		g.ApplyMove(m.From, m.To)
	}
	m, ok := book.Move(g)
	if !ok {
		t.Fatal("no book move for the position after 4.Ba4")
	}
	if got := m.UCI(); got != "g8f6" {
		t.Errorf("book answers %s, want g8f6: b5 was played 12 times to Nf6's 200 and cannot be trusted over it on score alone", got)
	}
}
