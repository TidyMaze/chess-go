package engine

import (
	"testing"

	"chess/game"
)

// championBenchFENs are openings, middlegames and two endgames from
// openings.txt and play, so no single phase decides the profile.
var championBenchFENs = []string{
	"r1b2rk1/1p2bppp/p1nppn2/q7/2P1P3/N1N5/PP2BPPP/R1BQ1RK1 w - - 0 1",
	"r2qk2r/3n2p1/1pp1p3/3pPpb1/P2P1nBp/1NB4P/1PP2P2/R3QR1K w kq f6 0 1",
	"r1bqk1nr/pp1p1pb1/2nQp1pp/8/2B1P3/2N2N2/PP3PPP/R1B1K2R b KQkq - 0 1",
	"r3kb1r/p1p1qp1p/b1pp1np1/4P3/2P2P2/1P6/P3BQPP/RNB1K2R b KQkq - 0 1",
	"r1bqk2r/pp1nbppp/4pn2/2pp4/3P4/2PBPN2/PP1N1PPP/R1BQ1RK1 b kq - 0 1",
	"r3k2r/2pq1pp1/pbp5/3pPb1p/Q2P4/B1P2N2/P4PPP/R4RK1 b kq - 0 1",
	"8/5pk1/6p1/3P4/5P2/6PK/8/8 w - - 0 1",
	"8/8/4k3/3r4/8/3K4/4R3/8 w - - 0 1",
}

// BenchmarkChampionSearch searches the way the champion plays: its network and
// feature list at a fixed depth, one thread, no book, a fresh table per search.
func BenchmarkChampionSearch(b *testing.B) {
	c := ReadChampion("../champion.json")
	c.NetFile = "../champion_net.json"
	c.Book = ""
	p, err := c.PlayerOrError()
	if err != nil {
		b.Skipf("champion: %v", err)
	}
	p.TimeBudget, p.Threads, p.Depth = 0, 1, 8
	nodes := 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, fen := range championBenchFENs {
			g, err := game.ParseFEN(fen)
			if err != nil {
				b.Fatal(err)
			}
			SeedRandom(1)
			ResetNodes()
			PlayerPick(p, g)
			nodes += TotalNodes()
		}
	}
	b.ReportMetric(float64(nodes)/b.Elapsed().Seconds()/1000, "knps")
	b.ReportMetric(float64(nodes)/float64(b.N), "nodes/op")
}
