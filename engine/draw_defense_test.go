package engine

import (
	"testing"

	"chess/game"
)

// TestForcesStalemateWhenLosing asserts that when the engine is completely
// losing (e.g. down a Queen with mate threatened) and has a tactical way to
// force an immediate stalemate draw, it chooses the stalemate over passive
// defense that gets mated.
func TestForcesStalemateWhenLosing(t *testing.T) {
	tests := []struct {
		name string
		fen  string
		want string
	}{
		{
			// White: Kh1, Rg6 (down Queen + pawn, mate threatened).
			// Black: Kh3, Qe3, pawn h4.
			// Only 1. Rg3+! forces draw by stalemate (either Qxg3 or Kxg3 leaves Kh1 stalemated).
			// Any other rook move gets mated in 2 to 3 plies.
			name: "White desperado rook forces stalemate",
			fen:  "8/8/6R1/8/7p/4q2k/8/7K w - - 0 1",
			want: "g6g3",
		},
		{
			// Black: Kh8, Rg3 (down Queen + pawn, White threatens mate on g7/h7).
			// White: Kh6, Qe6, pawn h5.
			// Only 1... Rg6+! forces draw by stalemate (either Qxg6 or Kxg6 leaves Kh8 stalemated).
			name: "Black desperado rook forces stalemate",
			fen:  "7k/8/4Q2K/7P/8/6r1/8/8 b - - 0 1",
			want: "g3g6",
		},
	}

	champ := ReadChampion("champion_bot.json")
	p, err := champ.PlayerOrError()
	if err != nil {
		t.Fatalf("PlayerOrError: %v", err)
	}
	p.TimeBudget = 0
	p.Depth = 4

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, err := game.ParseFEN(tc.fen)
			if err != nil {
				t.Fatalf("ParseFEN: %v", err)
			}
			tt := NewTranspositionTable(p.TTBits)
			m, ok := PlayerPickWith(p, g, tt)
			if !ok {
				t.Fatalf("no move returned for %s", tc.name)
			}
			if m.UCI() != tc.want {
				t.Errorf("%s: engine picked %s, want %s (forced stalemate)", tc.name, m.UCI(), tc.want)
			}
		})
	}
}

// TestForcesPerpetualDrawWhenLosing asserts that when down massive material
// (down a Queen and Rook vs Queen) and facing mate-in-1, the engine chooses
// a checking move to force perpetual check rather than passive moves that get mated.
func TestForcesPerpetualDrawWhenLosing(t *testing.T) {
	// White: Kg1, Qa8. Black: Kg8, Qd2, Rf2.
	// Black threatens Qxg2# mate in 1.
	// Only checking moves (1. Qb8+ or 1. Qe8+) prevent immediate checkmate and force perpetual check.
	fen := "Q5k1/8/8/8/8/8/3q1r2/6K1 w - - 0 1"
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	champ := ReadChampion("champion_bot.json")
	p, err := champ.PlayerOrError()
	if err != nil {
		t.Fatalf("PlayerOrError: %v", err)
	}
	p.TimeBudget = 0
	p.Depth = 4

	tt := NewTranspositionTable(p.TTBits)
	m, ok := PlayerPickWith(p, g, tt)
	if !ok {
		t.Fatal("no move returned")
	}
	if m.UCI() != "a8b8" && m.UCI() != "a8c8" && m.UCI() != "a8e8" {
		t.Errorf("engine picked %s, want a checking move (a8b8, a8c8, or a8e8) to force perpetual draw and prevent Qxg2#", m.UCI())
	}
}
