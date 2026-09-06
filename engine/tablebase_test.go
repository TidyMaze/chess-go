package engine

import (
	"path/filepath"
	"testing"

	"chess/board"
	"chess/game"
)

func krvk() []board.ColoredPiece {
	return []board.ColoredPiece{
		{Color: board.White, Type: board.King},
		{Color: board.White, Type: board.Rook},
		{Color: board.Black, Type: board.King},
	}
}

func setOf(t *testing.T, pieces []board.ColoredPiece) *TablebaseSet {
	t.Helper()
	// Bare kings first: every other configuration can be captured down to
	// it, and generating without that prior makes captures unresolvable.
	return BuildTablebases([][]board.ColoredPiece{
		{{Color: board.White, Type: board.King}, {Color: board.Black, Type: board.King}},
		pieces,
	}, nil)
}

// King and rook against king is a forced win. This is the ending the
// engine already converts, but only 88% of the time, which is the whole
// reason for the table.
func TestKingAndRookAgainstKingIsWonForTheStrongSide(t *testing.T) {
	set := setOf(t, krvk())
	if set.Len() == 0 {
		t.Fatal("generated an empty table")
	}
	// Every fixture must be a legal position: the rook may not be giving
	// check when it is White to move, because that would mean Black was
	// left in check by White's previous move. The generator rejects such
	// positions, and the first version of this test failed on three of
	// them before the fixtures were fixed rather than the code.
	for _, fen := range []string{
		"7k/8/8/8/8/8/8/K5R1 w - - 0 1",
		"8/8/4k3/8/8/8/8/K6R w - - 0 1",
		"8/8/8/3k4/8/8/R7/4K3 w - - 0 1",
	} {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		score, ok := set.Probe(&g.Board, g.Turn)
		if !ok {
			t.Errorf("%s: not in the table", fen)
			continue
		}
		if score <= 0 {
			t.Errorf("%s: scored %.1f, but king and rook against king is won for White", fen, score)
		}
	}
}

// The same ending with Black to move is still won for White, so from
// Black's point of view it is lost. A table that got this backwards would
// make the engine throw away won endings.
func TestTablebaseScoresFromWhitesPointOfView(t *testing.T) {
	set := setOf(t, krvk())
	g, err := game.ParseFEN("8/8/4k3/8/8/8/8/K6R b - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	score, ok := set.Probe(&g.Board, g.Turn)
	if !ok {
		t.Fatal("not in the table")
	}
	if score <= 0 {
		t.Errorf("scored %.1f with Black to move: White is a rook up and winning, "+
			"so the score must still be positive", score)
	}
}

// King against king cannot be won by anyone. A table claiming otherwise
// would have the engine chasing wins that do not exist.
func TestBareKingsAreDrawn(t *testing.T) {
	set := setOf(t, []board.ColoredPiece{
		{Color: board.White, Type: board.King},
		{Color: board.Black, Type: board.King},
	})
	g, _ := game.ParseFEN("8/8/8/3k4/8/8/8/K7 w - - 0 1")
	if score, ok := set.Probe(&g.Board, g.Turn); ok && score != 0 {
		t.Errorf("king against king scored %.1f, want a draw", score)
	}
}

// A closer mate must score higher than a distant one, or the search has
// no reason to finish the game and will shuffle until the fifty-move rule
// ends a won ending in a draw.
func TestCloserMatesScoreHigher(t *testing.T) {
	for _, c := range []struct{ near, far int16 }{{1, 9}, {3, 4}, {2, 30}} {
		if dtmToScore(c.near) <= dtmToScore(c.far) {
			t.Errorf("mate in %d scored %.3f and mate in %d scored %.3f",
				c.near, dtmToScore(c.near), c.far, dtmToScore(c.far))
		}
	}
	// And losing must be worse than drawing, whatever the distance.
	if dtmToScore(-20) >= 0 || dtmToScore(0) != 0 {
		t.Errorf("a lost position scored %.3f and a draw %.3f",
			dtmToScore(-20), dtmToScore(0))
	}
}

// The whole table must survive a round trip, since it is generated once
// and read by every process afterwards.
func TestTablebaseRoundTrips(t *testing.T) {
	set := setOf(t, krvk())
	path := filepath.Join(t.TempDir(), "tb.bin")
	if err := set.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTablebases(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != set.Len() {
		t.Fatalf("loaded %d entries, saved %d", got.Len(), set.Len())
	}
	g, _ := game.ParseFEN("8/8/4k3/8/8/8/8/K6R w - - 0 1")
	a, ok1 := set.Probe(&g.Board, g.Turn)
	b, ok2 := got.Probe(&g.Board, g.Turn)
	if !ok1 || !ok2 || a != b {
		t.Errorf("probe gave %.3f (%v) before saving and %.3f (%v) after", a, ok1, b, ok2)
	}
}

// A position with material the table does not cover must miss cleanly.
func TestProbeMissesUncoveredMaterial(t *testing.T) {
	set := setOf(t, krvk())
	if _, ok := set.Probe(&game.New().Board, board.White); ok {
		t.Error("the starting position was answered by a three-piece table")
	}
}

// King and pawn against king must contain wins.
//
// The first version of this generator produced a KPvK table with zero
// decisive positions and reported it as a success. Every win in that
// ending runs through a promotion, and a promotion changes the material,
// which the generator treated as leaving the table. A configuration that
// comes out uniformly drawn is a failing result, not a passing one, so
// this asserts the shape of the answer and not just that a file appeared.
func TestKingAndPawnAgainstKingContainsWins(t *testing.T) {
	set := BuildTablebases([][]board.ColoredPiece{
		{{Color: board.White, Type: board.King}, {Color: board.Black, Type: board.King}},
		{{Color: board.White, Type: board.King}, {Color: board.White, Type: board.Queen},
			{Color: board.Black, Type: board.King}},
		{{Color: board.White, Type: board.King}, {Color: board.White, Type: board.Pawn},
			{Color: board.Black, Type: board.King}},
	}, nil)
	if set.byKey["KPvK"] == nil || len(set.byKey["KPvK"].dtm) == 0 {
		t.Fatal("king and pawn against king has no decisive positions at all")
	}

	// A pawn on the seventh with the king in front of it is a trivial win.
	g, err := game.ParseFEN("8/3P4/3K4/8/8/8/8/7k w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	score, ok := set.Probe(&g.Board, g.Turn)
	if !ok {
		t.Fatal("a won king and pawn ending is not in the table")
	}
	if score <= 0 {
		t.Errorf("scored %.2f: a pawn on the seventh supported by its king is winning", score)
	}
}

// The classic drawn case: the defending king is in front of the pawn on
// the queening square. The engine must not think it is winning here.
func TestKingAndPawnDrawnOppositionIsNotAWin(t *testing.T) {
	set := BuildTablebases([][]board.ColoredPiece{
		{{Color: board.White, Type: board.King}, {Color: board.Black, Type: board.King}},
		{{Color: board.White, Type: board.King}, {Color: board.White, Type: board.Queen},
			{Color: board.Black, Type: board.King}},
		{{Color: board.White, Type: board.King}, {Color: board.White, Type: board.Pawn},
			{Color: board.Black, Type: board.King}},
	}, nil)
	// Black king on a8 in front of an a-pawn: a rook pawn with the
	// defender in the corner is drawn however White plays.
	g, err := game.ParseFEN("k7/P7/K7/8/8/8/8/8 b - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	if score, ok := set.Probe(&g.Board, g.Turn); ok && score > 0 {
		t.Errorf("scored %.2f: a rook pawn with the defending king in the corner is drawn", score)
	}
}

// The evaluation's colour argument is the side the score is for, which
// during a search is the root's colour and never changes. The tablebase
// index needs the side to move, which changes every ply. Confusing the
// two indexed the wrong entry on about half of all nodes.
func TestTablebaseProbeUsesTheSideToMoveNotThePerspective(t *testing.T) {
	tb := setOf(t, krvk())
	// Black to move, White winning. The score is requested from White's
	// perspective, which is not the side to move.
	g, err := game.ParseFEN("8/8/4k3/8/8/8/8/K6R b - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	ev := &Eval{Weights: DefaultWeights(), UsePST: true, Tablebases: tb, STM: g.Turn}
	withSTM := PositionScoreEval(&g.Board, board.White, ev)

	// The same evaluation with the side to move left at its zero value,
	// which is what the bug did.
	evWrong := &Eval{Weights: DefaultWeights(), UsePST: true, Tablebases: tb, STM: board.White}
	wrong := PositionScoreEval(&g.Board, board.White, evWrong)

	if withSTM <= 0 {
		t.Errorf("scored %.2f: White is a rook up and winning", withSTM)
	}
	if withSTM == wrong {
		t.Error("the score did not depend on the side to move, so the probe is ignoring it")
	}
}

// The evaluation answers from `color`'s point of view. Every branch of it
// negates for Black, and the tablebase branch did not, so the exact score
// was inverted in every game the engine played as Black.
func TestTablebaseScoreFollowsTheRequestedPerspective(t *testing.T) {
	tb := setOf(t, krvk())
	g, err := game.ParseFEN("8/8/4k3/8/8/8/8/K6R w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	ev := &Eval{Weights: DefaultWeights(), UsePST: true, Tablebases: tb, STM: g.Turn}
	white := PositionScoreEval(&g.Board, board.White, ev)
	black := PositionScoreEval(&g.Board, board.Black, ev)
	if white <= 0 {
		t.Errorf("from White: %.2f, but White is a rook up and winning", white)
	}
	if black >= 0 {
		t.Errorf("from Black: %.2f, but Black is losing; the score must flip with "+
			"the perspective as every other evaluation term does", black)
	}
}
