package game

import "testing"

// A move sent out (UCI bestmove, a lichess post, a game record) needs the
// promotion letter even for a queen: "e7e8" is not a legal UCI move, and
// GUIs and lichess reject it.
func TestMoveUCINamesThePromotionPiece(t *testing.T) {
	for _, tc := range []struct{ fen, in, want string }{
		{"8/4P1k1/8/8/8/8/6K1/8 w - - 0 1", "e7e8", "e7e8q"},
		{"8/4P1k1/8/8/8/8/6K1/8 w - - 0 1", "e7e8q", "e7e8q"},
		{"8/4P1k1/8/8/8/8/6K1/8 w - - 0 1", "e7e8n", "e7e8n"},
		{"8/4P1k1/8/8/8/8/6K1/8 w - - 0 1", "g2g3", "g2g3"},
		{"4k3/8/8/8/8/8/p7/4K3 b - - 0 1", "a2a1", "a2a1q"},
		{"4k3/8/8/8/8/p7/8/4K3 b - - 0 1", "a3a2", "a3a2"},
	} {
		g, err := ParseFEN(tc.fen)
		if err != nil {
			t.Fatal(err)
		}
		m, _ := MoveFromUCI(tc.in)
		if got := g.MoveUCI(m); got != tc.want {
			t.Errorf("%s in %s: MoveUCI = %q, want %q", tc.in, tc.fen, got, tc.want)
		}
	}
}
