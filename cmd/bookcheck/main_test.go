package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chess/engine"
	"chess/game"
)

func TestReadBookParsesFENAndMoveAndSkipsRubbish(t *testing.T) {
	const body = `rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1|e2e4

r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R w KQkq - 4 3|f1b5
a line with no separator
`
	path := filepath.Join(t.TempDir(), "book.txt")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readBook(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d entries, want 2 (the blank line and the separatorless line are not entries): %v", len(got), got)
	}
	if got[0].move != "e2e4" || !strings.HasPrefix(got[0].fen, "rnbqkbnr/") {
		t.Errorf("first entry is %+v", got[0])
	}
	if got[1].move != "f1b5" {
		t.Errorf("second entry move is %q, want f1b5", got[1].move)
	}
}

func TestReadBookReportsAMissingFile(t *testing.T) {
	if _, err := readBook(filepath.Join(t.TempDir(), "absent.txt")); err == nil {
		t.Error("a missing book file was not reported")
	}
}

// ApplyMove reports nothing, so an entry filed under the wrong position
// would otherwise be played silently and scored as if it were real.
func TestIsLegalRejectsAMoveTheBookFiledWrong(t *testing.T) {
	g := game.New()
	e4, ok := game.MoveFromUCI("e2e4")
	if !ok {
		t.Fatal("e2e4 did not parse")
	}
	if !isLegal(g, e4) {
		t.Error("e2e4 is legal from the start position")
	}
	silly, ok := game.MoveFromUCI("e2e5")
	if !ok {
		t.Fatal("e2e5 did not parse")
	}
	if isLegal(g, silly) {
		t.Error("e2e5 is not legal from the start position but was accepted")
	}
}

// The judge must not consult the book it is judging, or the engine would
// answer with the very move in question and agree with itself every time.
func TestAtDepthClearsTheBookAndFixesTheDepth(t *testing.T) {
	p := engine.Player{Depth: 2, TimeBudget: 99, Threads: 8, Book: &engine.Book{}}
	got := atDepth(p, 7)
	if got.Book != nil {
		t.Error("the book was left attached, so the engine would answer from the book being judged")
	}
	if got.Depth != 7 {
		t.Errorf("depth %d, want 7", got.Depth)
	}
	if !got.Iterative {
		t.Error("not iterative: the fixed depth path returns no score at all, so nothing could be compared")
	}
	if got.TimeBudget != 0 {
		t.Errorf("time budget %v, want 0 so the judgement is reproducible", got.TimeBudget)
	}
}

func judgingPlayer() engine.Player {
	return engine.Player{Name: "judge", Depth: 3, Quiescence: true, TTBits: 16, Iterative: true}
}

// A book move the engine would have played itself costs nothing.
//
// The position has exactly one legal move, which matters: the engine picks
// at random among moves it scores equally, so asking it twice from the
// start position returns g1f3 once and b1c3 the next time and no test can
// pin agreement that way.
func TestJudgeScoresAnAgreedMoveAsNoLoss(t *testing.T) {
	// Black king a8, white queen b7 giving check and undefended, so taking
	// it is the only move that exists.
	const fen = "k7/1Q6/8/8/8/8/8/7K b - - 0 1"
	v, ok := judge(judgingPlayer(), entry{fen: fen, move: "a8b7"}, 3)
	if !ok {
		t.Fatal("the position could not be judged")
	}
	if !v.agreed {
		t.Fatalf("the only legal move was not counted as agreement: engine played %s", v.engineMove)
	}
	if v.loss != 0 {
		t.Errorf("agreement scored a loss of %v, want 0", v.loss)
	}
}

// And a book move that walks past a free queen costs a lot. The position
// is contrived on purpose: the first candidate for this test was a real
// book entry that looked like it hung a queen, and the engine scored it as
// barely worse than the alternatives because the queen was pinned and lost
// either way. A test needs a position where the right move is not in
// dispute.
func TestJudgeScoresAMissedWinAsALoss(t *testing.T) {
	// White rook a1, black queen a5 undefended on the open file, so Rxa5
	// wins it outright. The book instead shuffles the king.
	const fen = "4k3/8/8/q7/8/8/8/R3K3 w Q - 0 1"
	v, ok := judge(judgingPlayer(), entry{fen: fen, move: "e1e2"}, 3)
	if !ok {
		t.Fatal("the position could not be judged")
	}
	if v.agreed {
		t.Fatal("the engine was reported as choosing the king move itself")
	}
	if v.loss < 5.0 {
		t.Errorf("passing up a free queen scored a loss of %.2f pawns, want far more; engine wanted %s",
			v.loss, v.engineMove)
	}
}

func TestJudgeRejectsAnUnparseableEntry(t *testing.T) {
	if _, ok := judge(judgingPlayer(), entry{fen: "not a fen", move: "e2e4"}, 3); ok {
		t.Error("an unparseable FEN was judged")
	}
	if _, ok := judge(judgingPlayer(), entry{fen: game.New().FEN(), move: "zzzz"}, 3); ok {
		t.Error("an unparseable move was judged")
	}
	if _, ok := judge(judgingPlayer(), entry{fen: game.New().FEN(), move: "e2e5"}, 3); ok {
		t.Error("a move that is not legal in the position was judged")
	}
}
