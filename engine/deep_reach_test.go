package engine

import (
	"chess/game"
	"testing"
	"time"
)

func TestDeepReachExperiment(t *testing.T) {
	fen := "r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9"
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	p := Strong(1)
	p.TimeBudget = 1000 * time.Millisecond
	p.Threads = 8
	p.TTBits = 22
	p.ApplyFeatures("lmp,deeplmp,rfp,scaledlmr,countermove,see,historymalus,lmrtwostep,nullgate")

	t0 := time.Now()
	m, ok := PlayerPick(p, g)
	t.Logf("move=%v ok=%v elapsed=%v depth=%d nodes=%d knps=%.0f",
		m, ok, time.Since(t0), LastSearchDepth(), TotalNodes(),
		float64(TotalNodes())/time.Since(t0).Seconds()/1000)
}

func TestBaselineChampionDepthAt1s(t *testing.T) {
	fen := "r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9"
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	champ := ReadChampion("../champion.json")
	champ.NetFile = "../champion_net.json"
	p := champ.Player()
	p.TimeBudget = 1000 * time.Millisecond

	t0 := time.Now()
	m, ok := PlayerPick(p, g)
	t.Logf("CHAMPION: move=%v ok=%v elapsed=%v depth=%d nodes=%d knps=%.0f",
		m, ok, time.Since(t0), LastSearchDepth(), TotalNodes(),
		float64(TotalNodes())/time.Since(t0).Seconds()/1000)
}

