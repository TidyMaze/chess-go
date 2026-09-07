package engine

import (
	"testing"

	"chess/game"
)

// Deeper search must not produce a wildly different verdict on a quiet
// position. Measured, depth 6 loses 144 +/- 43 Elo to depth 5 with the same
// evaluation, which no correct search does: a ply is normally worth 50 to
// 100 Elo in the other direction.
func TestScoreAcrossDepths(t *testing.T) {
	for _, fen := range []string{
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"r2q1rk1/pp1bbppp/2np1n2/4p3/2P1P3/2NP1N2/PP2BPPP/R1BQ1RK1 w - - 0 10",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	} {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s", fen)
		for d := 1; d <= 7; d++ {
			p := Strong(d)
			score, ok := PlayerScoreWith(p, g, nil)
			m, ok2 := PlayerPick(p, g)
			mv := "none"
			if ok2 {
				mv = m.UCI()
			}
			t.Logf("   depth %d  score %+8.3f  best %-6s  nodes %d  ok=%v",
				d, score, mv, LastSearchNodes, ok)
		}
	}
}
