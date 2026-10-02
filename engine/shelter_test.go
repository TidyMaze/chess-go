package engine

import (
	"chess/game"
	"testing"
)

func TestKingShelterAvoidsBlunderingPawns(t *testing.T) {
	// aJeii27Z move 10: Black castled kingside (g8).
	// Pawns on f7, g7, h6.
	// 10... g5 blunders king shelter and invites kingside attack.
	g, err := game.ParseFEN("r1bq1rk1/ppp1npb1/3p1npp/P2Pp3/2P1P3/2N2N2/1P2BPPP/R1BQ1RK1 b - - 0 10")
	if err != nil {
		t.Fatal(err)
	}
	c := ReadChampion("../champion_bot.json")
	c.NetFile = "../champion_net.json"
	c.Book = ""
	p, err := c.PlayerOrError()
	if err != nil {
		t.Fatal(err)
	}
	p.Depth = 6
	p.TimeBudget = 0
	p.Threads = 1
	p.Book = nil
	p.ApplyFeatures("shelter")

	m, ok := p.pick(g)
	if !ok {
		t.Fatal("no move chosen")
	}
	t.Logf("chosen move: %s", m.UCI())
	if m.UCI() == "g6g5" || m.UCI() == "g7g5" {
		t.Errorf("engine chose suicidal shelter weakening 10... %s", m.UCI())
	}
}

func TestKingShelterAvoidsBlunderingPawnsVIv1K6hG(t *testing.T) {
	// VIv1K6hG move 10: Black castled kingside (g8).
	// Pawns on f7, g7, h6.
	// 10... g5 blunders king shelter and invites 11. Nxg5! piece sacrifice.
	g, err := game.ParseFEN("r1bq1rk1/pp3pp1/2nb1n1p/2p1p3/2BpP2B/3P1NN1/PPP2PPP/R2Q1RK1 b - - 1 10")
	if err != nil {
		t.Fatal(err)
	}
	c := ReadChampion("../champion_bot.json")
	c.NetFile = "../champion_net.json"
	c.Book = ""
	p, err := c.PlayerOrError()
	if err != nil {
		t.Fatal(err)
	}
	p.Depth = 6
	p.TimeBudget = 0
	p.Threads = 1
	p.Book = nil
	p.ApplyFeatures("shelter")

	m, ok := p.pick(g)
	if !ok {
		t.Fatal("no move chosen")
	}
	t.Logf("chosen move: %s", m.UCI())
	if m.UCI() == "g7g5" {
		t.Errorf("engine chose suicidal shelter weakening 10... g7g5")
	}
}

func TestShelterMatch(t *testing.T) {
	c := ReadChampion("../champion_bot.json")
	c.NetFile = "../champion_net.json"
	c.Book = ""
	base, err := c.PlayerOrError()
	if err != nil {
		t.Fatal(err)
	}
	base.Depth = 3
	base.TimeBudget = 0
	base.Threads = 1
	base.Book = nil
	base.Name = "base"

	cand := base
	cand.Name = "shelter"
	cand.ApplyFeatures("shelter")

	res := PlayMatch(cand, base, 100, 200)
	t.Logf("Shelter vs Base (100 games, depth 3): W-D-L %d-%d-%d (score %.3f), Elo %+d +/- %d",
		res.Wins, res.Draws, res.Losses, res.Score(), res.Elo(), res.EloMargin())
	if res.Score() < 0.45 {
		t.Errorf("Shelter regressed: score %.3f", res.Score())
	}
}

func BenchmarkChampionSearchWithShelter(b *testing.B) {
	c := ReadChampion("../champion.json")
	c.NetFile = "../champion_net.json"
	c.Book = ""
	p, err := c.PlayerOrError()
	if err != nil {
		b.Skipf("champion: %v", err)
	}
	p.ApplyFeatures("shelter")
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

func BenchmarkChampionSearchWithPassedKing(b *testing.B) {
	c := ReadChampion("../champion.json")
	c.NetFile = "../champion_net.json"
	c.Book = ""
	p, err := c.PlayerOrError()
	if err != nil {
		b.Skipf("champion: %v", err)
	}
	p.ApplyFeatures("passedking")
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
