package engine

import (
	"testing"

	"chess/game"
)

// The king drive starts at +4. Most won endgames we drew against Stockfish
// were scored +2 to +4, below it, so drive2 starts it at +2, in endgames only:
// in a middlegame the bonus walks our own king toward the enemy's.
func TestDrive2LowersTheKingDriveThresholdInEndgamesOnly(t *testing.T) {
	endgame, _ := game.ParseFEN("8/8/4k3/8/8/3RK3/7P/7r w - - 0 1")
	opening, _ := game.ParseFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	cases := []struct {
		name  string
		board *game.Game
		ev    *Eval
		want  float64
	}{
		{"endgame, drive2 on", endgame, &Eval{Drive2: true}, 2},
		{"endgame, drive2 off", endgame, &Eval{}, 4},
		{"opening, drive2 on", opening, &Eval{Drive2: true}, 4},
		{"no eval", endgame, nil, 4},
	}
	for _, c := range cases {
		if got := kingDriveThreshold(&c.board.Board, c.ev); got != c.want {
			t.Errorf("%s: threshold %v, want %v", c.name, got, c.want)
		}
	}
	p := Player{}
	p.ApplyFeatures("drive2")
	if !evalForPlayer(p).Drive2 {
		t.Error("the drive2 feature does not reach the evaluation")
	}
}
