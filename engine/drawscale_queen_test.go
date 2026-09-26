package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// A queen against two minor pieces leads by less than four pawns of pieces
// and drawScale quartered it, yet the lichess tablebase wins 78, 71 and 59
// of 80 quiet positions against bishop and knight, two bishops and two
// knights. Three minors, or a rook and a minor, are a different ending.
func TestDrawScaleLetsAQueenBeatTwoMinors(t *testing.T) {
	cases := []struct {
		name, fen string
		want      float64 // factor applied to a +6 score for White
	}{
		{"queen against bishop and knight wins", "4k3/8/3bn3/8/8/8/8/4K2Q w - - 0 1", 1},
		{"queen against two bishops wins", "4k3/8/3bb3/8/8/8/8/4K2Q w - - 0 1", 1},
		{"queen against two knights wins", "4k3/8/3nn3/8/8/8/8/4K2Q w - - 0 1", 1},
		{"queen against three minors does not", "4k3/8/2nbn3/8/8/8/8/4K2Q w - - 0 1", 0.25},
		{"queen against rook and knight does not", "4k3/8/3rn3/8/8/8/8/4K2Q w - - 0 1", 0.25},
	}
	for _, c := range cases {
		g, err := game.ParseFEN(c.fen)
		if err != nil {
			t.Fatal(c.name, err)
		}
		if got := drawScale(&g.Board, 6, board.White); got != 6*c.want {
			t.Errorf("%s: +6 for White became %v, want %v", c.name, got, 6*c.want)
		}
	}
}
