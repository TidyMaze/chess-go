package lichessbot

import (
	"sort"
	"strings"
	"testing"

	"chess/board"
)

// The three games the bot lost on time right after an opponent
// underpromotion. Each replays the move list the way the bot does and
// checks the board, then the legal replies, against lichess's position.
func TestReplayKeepsAnOpponentUnderpromotion(t *testing.T) {
	for _, tc := range []struct {
		game, fen, moves string
		sq               board.Sq
		want             board.Piece
		legal            string
	}{
		// f8=N gives check; with a queen there the bot saw none and
		// posted h3h1.
		{"ymteJEvj", "8/R4P2/6k1/4p3/4b3/4P1Pr/p5r1/5K2 w - - 0 43", "f7f8n",
			board.Sq{File: 5, Rank: 7}, board.Piece{Color: board.White, Type: board.Knight},
			"g6f5 g6f6 g6g5 g6h5 g6h6"},
		// g1=B leaves White two king moves; a queen on g1 left none, so
		// the bot thought it was stalemated and posted nothing.
		{"JCaxxkC1", "8/7p/7P/5p2/4b3/8/4K1p1/2k5 b - - 1 68", "g2g1b",
			board.Sq{File: 6, Rank: 0}, board.Piece{Color: board.Black, Type: board.Bishop},
			"e2e1 e2f1"},
		{"i4FtWSpF", "3r2k1/4bp2/8/Q1p3q1/4R3/7p/PPP2K1p/8 b - - 1 33", "h2h1n",
			board.Sq{File: 7, Rank: 0}, board.Piece{Color: board.Black, Type: board.Knight},
			"f2e1 f2e2 f2f1 f2f3"},
	} {
		g, err := applyMovesString(tc.fen, tc.moves)
		if err != nil {
			t.Fatal(err)
		}
		if p, ok := g.Board.PieceAt(tc.sq); !ok || p != tc.want {
			t.Errorf("%s: after %s the bot's board has %+v (ok=%v), want %+v", tc.game, tc.moves, p, ok, tc.want)
		}
		var legal []string
		for _, m := range g.AllLegalMoves(g.Turn) {
			legal = append(legal, m.UCI())
		}
		sort.Strings(legal)
		if got := strings.Join(legal, " "); got != tc.legal {
			t.Errorf("%s: legal replies [%s], lichess has [%s]", tc.game, got, tc.legal)
		}
	}
}
