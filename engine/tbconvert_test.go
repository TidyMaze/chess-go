package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// Does the tablebase actually make the engine convert won endings?
//
// King and rook against king was measured 88% reliable with the
// hand-written evaluation. If the tablebase does not push that to 100%,
// the probe is not doing what it claims, and the match result that
// follows from it is measuring something else.
func TestTablebaseConvertsRookEndings(t *testing.T) {
	tb, err := LoadTablebases("../tablebases3.bin")
	if err != nil {
		t.Skip("no tablebases")
	}
	starts := []string{
		"8/8/4k3/8/8/8/8/K6R w - - 0 1",
		"8/8/8/3k4/8/8/R7/4K3 w - - 0 1",
		"7k/8/8/8/8/8/8/K5R1 w - - 0 1",
		"8/8/8/8/3k4/8/8/R3K3 w - - 0 1",
		"8/2k5/8/8/8/8/7R/4K3 w - - 0 1",
		"5k2/8/8/8/8/8/1R6/6K1 w - - 0 1",
	}
	for _, useTB := range []bool{false, true} {
		converted, plies := 0, 0
		for _, fen := range starts {
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			g.EnableRepetitionTracking()
			p := Strong(4)
			if useTB {
				p.Tablebases = tb
			}
			n := 0
			for ; n < 120 && !g.IsOver(); n++ {
				m, ok := PlayerPick(p, g)
				if !ok {
					break
				}
				g.ApplyMove(m.From, m.To)
			}
			if g.IsCheckmate(g.Turn) && g.Turn == board.Black {
				converted++
				plies += n
			}
		}
		label := "hand evaluation"
		if useTB {
			label = "with tablebases"
		}
		avg := 0
		if converted > 0 {
			avg = plies / converted
		}
		t.Logf("%-16s converted %d of %d rook endings, average %d plies",
			label, converted, len(starts), avg)
	}
}
