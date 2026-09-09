package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// SeedRandom must actually make a measurement reproducible.
//
// It called math/rand.Seed, which Go turned into a no-op: the global
// source is auto-seeded and the deprecated setter stopped taking effect,
// so every "seeded" run drew a different sequence. Two arms of an A/B
// then played different games while the harness reported them as paired,
// which is the worst failure mode a measurement harness has.
func TestSeedRandomMakesTheSequenceReproducible(t *testing.T) {
	draw := func(seed int64) []int {
		SeedRandom(seed)
		out := make([]int, 12)
		for i := range out {
			out[i] = randIntn(1000)
		}
		return out
	}
	a, b := draw(7), draw(7)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("the same seed drew %v then %v", a, b)
		}
	}
	if c := draw(8); c[0] == a[0] && c[1] == a[1] && c[2] == a[2] {
		t.Errorf("different seeds drew the same sequence: %v", c)
	}
}

// And a whole game must replay identically from the same seed.
func TestSeededGamesReplayIdentically(t *testing.T) {
	play := func(seed int64) int {
		SeedRandom(seed)
		g, err := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
		if err != nil {
			t.Fatal(err)
		}
		plies := 0
		playFrom(g, Strong(2), Strong(2), 40, func(*game.Game, int, board.Sq, board.Sq) { plies++ })
		return plies
	}
	if x, y := play(3), play(3); x != y {
		t.Errorf("the same seed played %d plies then %d", x, y)
	}
}
