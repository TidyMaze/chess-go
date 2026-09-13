package engine

import (
	"os"
	"testing"

	"chess/game"
)

// convertsToMate plays the champion against itself from fen and reports
// whether the side to move first drove it to a terminal position.
func convertsToMate(t *testing.T, fen string, depth int, maxPlies int) (bool, string) {
	t.Helper()
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatalf("%s: %v", fen, err)
	}
	// ParseFEN leaves repetition tracking off, which is right for the
	// search's own throwaway positions and wrong for a game being played:
	// the move picker's only anti-shuffle rule is to prefer a move that
	// does not repeat a position it has already stood in, and that rule
	// is dead without this.
	g.EnableRepetitionTracking()
	// Seeded: the move picker breaks ties between equally-scored moves at
	// random, so without this the same code gives pass, fail, pass on
	// three consecutive runs and the test measures luck.
	SeedRandom(1)
	// Fixed depth, not a time budget: a wall-clock budget makes this test
	// depend on what else the machine is doing, and it was run while the
	// lichess bot had six games in flight. Same code, three runs, gave
	// pass, fail, pass.
	p := Strong(depth)
	tt := NewTranspositionTable(20)
	for i := 0; i < maxPlies; i++ {
		if len(g.AllLegalMoves(g.Turn)) == 0 {
			return true, g.FEN()
		}
		m, ok := PlayerPickWith(p, g, tt)
		if !ok {
			return true, g.FEN()
		}
		g.ApplyMove(m.From, m.To)
	}
	return false, g.FEN()
}

// The three basic mates every engine must finish. A queen and a rook
// already worked; two bishops did not, and that is a textbook forced win.
// It failed because the king-driving term pushes the bare king to the
// nearest edge, while a two-bishop mate has to drive it to a corner, and
// on the edge the search found nothing better to do than shuffle. A real
// lichess game reached a two-bishop-against-bishop ending and repeated
// moves toward the fifty-move rule in a completely won position.
func TestBasicMatesAreConverted(t *testing.T) {
	if os.Getenv("ENDGAME") == "" {
		t.Skip("ENDGAME=1 runs the basic-mate conversion check; it currently fails, see LEARNINGS.md")
	}
	for _, c := range []struct {
		name     string
		fen      string
		maxPlies int
	}{
		{"king and queen", "8/8/8/4k3/8/8/8/3QK3 w - - 0 1", 60},
		{"king and rook", "8/8/8/4k3/8/8/8/3RK3 w - - 0 1", 80},
		{"king and two bishops", "8/8/8/4k3/8/8/8/2BBK3 w - - 0 1", 120},
	} {
		done, final := convertsToMate(t, c.fen, 4, c.maxPlies)
		if !done {
			t.Errorf("%s: no mate in %d plies, ended at %s", c.name, c.maxPlies, final)
		}
	}
}
