package engine

import (
	"testing"

	"chess/game"
)

// A paired-opening match game starts from a FEN. Without repetition
// tracking IsOver never sees a threefold, so a repeating game runs on to
// adjudication or the move cap, and the search is blind to the history.
func TestMatchOpeningGameEndsOnAThreefold(t *testing.T) {
	saved := MatchOpenings
	defer func() { MatchOpenings = saved }()
	MatchOpenings = []string{shuffleFEN}
	g := matchOpening(0)
	for _, s := range append(append([]string{}, shuffleMoves...), "g8h8") {
		m, ok := game.MoveFromUCI(s)
		if !ok {
			t.Fatalf("bad move %q", s)
		}
		g.Apply(m)
	}
	if !g.IsOver() {
		t.Errorf("the opening position occurred a third time and the game is not over (TrackRepetition=%v)", g.TrackRepetition)
	}
}
