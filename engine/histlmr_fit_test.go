package engine

import (
	"os"
	"testing"

	"chess/game"
)

// Fit the divisor by tree size at a fixed depth, which no machine load can
// tilt. Fewer nodes for the same depth is a smaller branching factor; the
// race decides whether the pruning it buys costs anything.
func TestFitHistLMRDivisor(t *testing.T) {
	wd, _ := os.Getwd()
	if err := os.Chdir(".."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	base := ReadChampion("champion_bot.json").Player()
	if base.HalfKP == nil {
		t.Skip("champion network did not load")
	}
	base.Book, base.Threads, base.Depth, base.Iterative = nil, 1, 8, true
	base.TimeBudget = 0

	run := func(on bool, divisor int) (nodes int, moves []string) {
		old := histLMRDivisor
		histLMRDivisor = divisor
		defer func() { histLMRDivisor = old }()
		p := base
		p.HistLMR = on
		p.HistGravity = true
		for _, fen := range correctnessPositions[:6] {
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			ResetNodes()
			m, _ := PlayerPickWith(p, g, NewTranspositionTable(p.TTBits))
			nodes += TotalNodes()
			moves = append(moves, m.UCI())
		}
		return nodes, moves
	}

	offNodes, offMoves := run(false, 0)
	t.Logf("%-10s %9d nodes   %v  (gravity on, LMR adjustment off)", "off", offNodes, offMoves)
	for _, d := range []int{1024, 2048, 4096, 8192, 16384} {
		n, mv := run(true, d)
		same := 0
		for i := range mv {
			if mv[i] == offMoves[i] {
				same++
			}
		}
		t.Logf("divisor %-6d %9d nodes  %+5.1f%%  same move in %d of %d", d, n,
			100*float64(n-offNodes)/float64(offNodes), same, len(mv))
	}
}
