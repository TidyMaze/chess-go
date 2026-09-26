package engine

import (
	"fmt"
	"testing"

	"chess/board"
)

// Only the part of the score above the material balance fades. A queen up
// at clock 90 still reads at least the queen, so no clock reset is worth a
// piece, while what the evaluation adds on top of it (here the king drive)
// fades without vanishing: shuffling still costs something, and the drive
// still points at the mate. Same from the losing side's point of view.
func TestFadeKeepsTheMaterialBalance(t *testing.T) {
	ev := &Eval{MaterialOnly: true}
	for _, c := range []struct {
		side, fen string
		color     board.Color
		sign      float64
	}{
		{"queen side", "4k3/8/8/8/8/8/8/Q3K3 w - - %d 1", board.White, 1},
		{"lone king side", "4k3/8/8/8/8/8/8/Q3K3 b - - %d 1", board.Black, -1},
	} {
		fresh := c.sign * evalPosition(mustFEN(t, fmt.Sprintf(c.fen, 0)), c.color, ev)
		late := c.sign * evalPosition(mustFEN(t, fmt.Sprintf(c.fen, 90)), c.color, ev)
		if fresh <= 9 {
			t.Fatalf("%s: a queen up with the king drive reads %v, want above 9", c.side, fresh)
		}
		if late <= 9 || late >= fresh {
			t.Errorf("%s: a queen up reads %v at clock 90 and %v at clock 0; want between the queen (9) and the fresh score", c.side, late, fresh)
		}
	}
}

// Below a winning edge the floor stays out of the way: a modest lead the
// score does not rate above its material (a fortress, a perpetual) must
// keep fading toward the draw, or the engine shuffles for fifty moves
// again, which is what the fade was written against. Rook against bishop,
// two pawns of material, no king drive.
func TestFadeStillPullsAModestLeadTowardTheDraw(t *testing.T) {
	ev := &Eval{MaterialOnly: true}
	fresh := evalPosition(mustFEN(t, "4k3/8/8/8/8/8/8/R3K2b w - - 0 1"), board.White, ev)
	late := evalPosition(mustFEN(t, "4k3/8/8/8/8/8/8/R3K2b w - - 90 1"), board.White, ev)
	if fresh != 2 || late >= fresh {
		t.Errorf("rook against bishop reads %v at clock 0 and %v at clock 90; want 2, then less", fresh, late)
	}
}

// Queen (or rook), bishop and knight against two pawns, from the bug hunt.
// The net is saturated here and values the bishop at about 0.3 pawns, while
// the fade took 4.4 pawns off +14 at clock 70 and 6.1 at clock 90. Bxg6, a
// bishop for a pawn, resets the clock, so the bot gave the piece away at
// depths 4 to 10 to get the fade back.
func TestFadeNeverPaysForGivingAPiece(t *testing.T) {
	for _, c := range []struct{ name, fen string }{
		{"queen", "8/6k1/5pp1/8/4B3/2N5/2K5/3Q4 w - - %d 80"},
		{"rook", "8/6k1/5pp1/8/4B3/2N5/2K5/7R w - - %d 80"},
	} {
		for _, clock := range []int{70, 90} {
			for _, depth := range []int{4, 6, 8} {
				p := botAtDepth(t, depth)
				SeedRandom(1)
				m, score, ok := PlayerPickScored(p, mustFEN(t, fmt.Sprintf(c.fen, clock)))
				if !ok || m.UCI() == "e4g6" {
					t.Errorf("%s, clock %d, depth %d: played %s (score %.2f), a bishop for a pawn to reset the clock", c.name, clock, depth, m.UCI(), score)
				}
			}
		}
	}
}
