package engine

import (
	"fmt"
	"os"
	"testing"
	"time"

	"chess/game"
)

// BenchmarkTimedMove10ms is the deployed champion playing 10 ms moves the
// way a gauntlet game does: one table per game reused across moves, one
// thread, book off so every move is a search. It exists to be profiled;
// the metrics it reports say how much of each 10 ms is search and how deep
// it gets, which is the number a 10 ms race is won or lost on.
func BenchmarkTimedMove10ms(b *testing.B) {
	for _, bits := range []uint{22, 20, 18, 16} {
		b.Run(fmt.Sprintf("tt=%d", bits), func(b *testing.B) {
			benchTimedMove(b, bits, 10*time.Millisecond, correctnessPositions, 6)
		})
	}
}

// BenchmarkTimedMove1s is the same at the budget the champion was rated at,
// on fewer positions so one iteration stays under half a minute.
func BenchmarkTimedMove1s(b *testing.B) {
	benchTimedMove(b, 22, time.Second, correctnessPositions[:8], 3)
}

func benchTimedMove(b *testing.B, ttBits uint, budget time.Duration, fens []string, movesPerPosition int) {
	wd, _ := os.Getwd()
	if err := os.Chdir(".."); err != nil {
		b.Fatal(err)
	}
	defer os.Chdir(wd)
	// CHESS_BENCH_CHAMPION races a variant against the deployed champion
	// without editing the deployed file.
	file := "champion_bot.json"
	if v := os.Getenv("CHESS_BENCH_CHAMPION"); v != "" {
		file = v
	}
	c := ReadChampion(file)
	p := c.Player()
	if p.HalfKP == nil {
		b.Skip("champion network did not load")
	}
	p.Book = nil
	p.Threads = 1
	p.TimeBudget = budget
	p.TTBits = ttBits

	var moves, nodes, depth int64
	var elapsed time.Duration
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, fen := range fens {
			g, err := game.ParseFEN(fen)
			if err != nil {
				b.Fatal(err)
			}
			g.EnableRepetitionTracking()
			tt := NewTranspositionTable(p.TTBits)
			for k := 0; k < movesPerPosition && !g.IsOver(); k++ {
				ResetNodes()
				start := time.Now()
				m, ok := PlayerPickWith(p, g, tt)
				el := time.Since(start)
				if !ok {
					break
				}
				g.ApplyMove(m.From, m.To)
				moves++
				nodes += int64(TotalNodes())
				depth += int64(LastSearchDepth())
				elapsed += el
			}
		}
	}
	b.StopTimer()
	if moves > 0 {
		b.ReportMetric(float64(nodes)/float64(moves), "nodes/move")
		b.ReportMetric(float64(depth)/float64(moves), "depth/move")
		b.ReportMetric(float64(elapsed.Microseconds())/float64(moves), "us/move")
		b.ReportMetric(float64(nodes)/elapsed.Seconds()/1000, "knps")
	}
}
