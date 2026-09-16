package game

import (
	"math/rand"
	"testing"

	"chess/board"
	"chess/moves"
)

// The alignment prefilter in moves.PinnedSquares may skip ray walks, never
// change the answer. Random real games, not a thinned start position: a pin
// needs an enemy slider on the king's line with one own man between, which
// the opening layout never produces, and a test that never meets a pin
// tests nothing.
func TestPinPrefilterAgreesWithTheFullWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	pinsSeen := 0
	for gameNo := 0; gameNo < 150; gameNo++ {
		g := New()
		for ply := 0; ply < 40; ply++ {
			legal := g.AllLegalMoves(g.Turn)
			if len(legal) == 0 {
				break
			}
			m := legal[rng.Intn(len(legal))]
			g.ApplyMove(m.From, m.To)
			for _, c := range []board.Color{board.White, board.Black} {
				got := moves.PinnedSquares(&g.Board, c)
				want := moves.PinnedSquaresUnfiltered(&g.Board, c)
				if got != want {
					t.Fatalf("game %d ply %d, %v: filtered %v, full walk %v\n%s", gameNo, ply, c, got, want, g.FEN())
				}
				if want != 0 {
					pinsSeen++
				}
			}
		}
	}
	if pinsSeen == 0 {
		t.Fatal("no position with a pin was ever reached, so the prefilter was never exercised")
	}
	t.Logf("%d positions with a pin", pinsSeen)
}
