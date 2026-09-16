package engine

import (
	"os"
	"testing"
	"time"

	"chess/game"
)

// Where beta cutoffs happen decides the branching factor: a cutoff on the
// first move searched costs one subtree, a cutoff on the fifth costs five.
// A well ordered engine cuts on the first move around 90% of the time.
func TestReportCutoffDistribution(t *testing.T) {
	wd, _ := os.Getwd()
	if err := os.Chdir(".."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	p := ReadChampion("champion_bot.json").Player()
	if p.HalfKP == nil {
		t.Skip("champion network did not load")
	}
	p.Book, p.Threads, p.TimeBudget = nil, 1, 100*time.Millisecond

	ResetOrderingStats()
	OrderingStats = true
	defer func() { OrderingStats = false }()
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		tt := NewTranspositionTable(p.TTBits)
		PlayerPickWith(p, g, tt)
	}
	rate, dist, total := OrderingReport()
	if total == 0 {
		t.Fatal("no cutoffs recorded")
	}
	cum := int64(0)
	for i, n := range dist {
		cum += n
		label := "move " + string(rune('1'+i))
		if i == len(dist)-1 {
			label = "move 8+"
		}
		t.Logf("%-8s %9d  %5.1f%%  (cumulative %5.1f%%)", label, n,
			100*float64(n)/float64(total), 100*float64(cum)/float64(total))
	}
	t.Logf("first-move cutoff rate %.1f%% over %d cutoffs", 100*rate, total)
}
