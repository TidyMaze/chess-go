package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

func TestDrawScaleFeatureReachesTheSearchEvaluation(t *testing.T) {
	g, _ := game.ParseFEN("8/8/4k3/8/8/3RK3/8/7b w - - 0 1")
	off := evalPositionFor(g, board.White, board.White, &Eval{UsePST: true})
	on := evalPositionFor(g, board.White, board.White, &Eval{UsePST: true, DrawScale: true})
	if off <= 0 || on != off/4 {
		t.Errorf("rook against bishop: %v without drawscale, %v with it, want a quarter", off, on)
	}
	p := Player{}
	p.ApplyFeatures("drawscale")
	if !evalForPlayer(p).DrawScale {
		t.Error("the drawscale feature does not reach the evaluation")
	}
}

// Endgames the engine scored at +3 to +6 against Stockfish 2700 and drew.
func TestDrawScaleShrinksMaterialThatCannotWin(t *testing.T) {
	cases := []struct {
		name, fen string
		want      float64 // factor applied to a +6 score for White
	}{
		{"rook against bishop", "8/8/4k3/8/8/3RK3/8/7b w - - 0 1", 0.25},
		{"rook and bishop against rook", "8/8/4k3/4r3/8/2BRK3/8/8 w - - 0 1", 0.25},
		{"lone knight cannot mate", "8/8/4k3/8/8/3NK3/8/8 w - - 0 1", 0},
		{"two knights cannot force mate", "8/8/4k3/8/8/2NNK3/8/8 w - - 0 1", 0},
		{"opposite-coloured bishops", "2b3k1/pp6/8/8/8/2P5/PP6/2B3K1 w - - 0 1", 0.5},
		{"same-coloured bishops", "5bk1/pp6/8/8/8/2P5/PP6/2B3K1 w - - 0 1", 1},
		{"bishop and knight mate", "8/8/4k3/8/8/2BNK3/8/8 w - - 0 1", 1},
		{"queen against rook wins", "8/8/4k3/4r3/8/3QK3/8/8 w - - 0 1", 1},
		{"pawns keep the win alive", "8/8/4k3/8/8/3RK3/7P/7b w - - 0 1", 1},
		{"opening", "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", 1},
	}
	for _, c := range cases {
		g, err := game.ParseFEN(c.fen)
		if err != nil {
			t.Fatal(c.name, err)
		}
		if got := drawScale(&g.Board, 6, board.White); got != 6*c.want {
			t.Errorf("%s: +6 for White became %v, want %v", c.name, got, 6*c.want)
		}
		// The rule follows the side that is ahead, whichever it is.
		if got := drawScale(&g.Board, -6, board.Black); got != -6*c.want && c.want != 1 {
			// Black is behind here, so a -6 for Black is White's +6 again.
			t.Errorf("%s: -6 for Black became %v, want %v", c.name, got, -6*c.want)
		}
	}
}
