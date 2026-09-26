package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// A rook pawn whose queening corner the bishop cannot cover is a dead draw
// once the lone king is on or next to that corner: nothing drives it out.
// The network scores it at +6 to +9, and the engine traded rooks into it,
// Rxg7 in 7k/6r1/8/5K1P/8/3B4/8/6R1 w at depth 4, 8 and 1 s (+6.5 to
// +8.8), where the tablebase has seven winning moves and Rxg7 draws.
// Every verdict below is the lichess tablebase's.
func TestDrawScaleKnowsTheWrongRookPawn(t *testing.T) {
	cases := []struct {
		name, fen string
		strong    board.Color
		want      float64 // factor applied to +6 for the strong side
	}{
		{"wrong bishop, king in the corner (draw)", "7k/8/8/7P/8/8/8/4KB2 w - - 0 1", board.White, 0},
		{"wrong bishop, king next to the corner (draw)", "8/6k1/8/7P/8/8/8/4KB2 w - - 0 1", board.White, 0},
		{"doubled rook pawns, wrong bishop (draw)", "7k/8/8/7P/7P/8/8/4KB2 w - - 0 1", board.White, 0},
		{"black's wrong bishop and a-pawn (draw)", "2b1k3/8/8/8/p7/8/8/K7 w - - 0 1", board.Black, 0},
		{"bare rook pawn, king in the corner (draw)", "7k/8/8/7P/8/8/8/4K3 w - - 0 1", board.White, 0},
		{"b-pawn is not a rook pawn (win)", "1k6/8/8/1P6/8/8/8/4KB2 w - - 0 1", board.White, 1},
		{"right bishop covers the corner (win)", "7k/8/8/7P/8/8/8/4K1B1 w - - 0 1", board.White, 1},
		{"king too far from the corner (win)", "8/8/8/3k3P/8/8/8/4KB2 w - - 0 1", board.White, 1},
	}
	for _, c := range cases {
		g, err := game.ParseFEN(c.fen)
		if err != nil {
			t.Fatal(c.name, err)
		}
		if got := drawScale(&g.Board, 6, c.strong); got != 6*c.want {
			t.Errorf("%s: +6 became %v, want %v", c.name, got, 6*c.want)
		}
	}
}

// The bot's own configuration no longer trades rooks into the dead draw.
func TestBotDoesNotTradeIntoTheWrongRookPawn(t *testing.T) {
	champ := ReadChampion("../champion_bot.json")
	champ.NetFile = "../champion_net.json"
	champ.Book = ""
	p, err := champ.PlayerOrError()
	if err != nil {
		t.Fatal(err)
	}
	p.Threads, p.TimeBudget, p.Depth = 1, 0, 4
	g, err := game.ParseFEN("7k/6r1/8/5K1P/8/3B4/8/6R1 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := PlayerPick(p, g)
	if !ok || m.UCI() == "g1g7" {
		t.Errorf("played %s: Rxg7 Kxg7 is the wrong-bishop draw, seven rook moves win", m.UCI())
	}
}
