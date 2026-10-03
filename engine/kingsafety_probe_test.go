package engine

import (
	"testing"

	"chess/game"
)

func TestVIv1K6hGKingSafetyMoveChoice(t *testing.T) {
	// VIv1K6hG:15... Black must play 15... Bxg5 (trading attacker) instead of 15... Ne3? (greedy blunder).
	g, err := game.ParseFEN("r1bqr1k1/pp2bp2/2n5/2p1p1B1/2BpPPn1/3P2N1/PPP3PP/R2Q1RK1 b - - 2 15")
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
	p.Depth = 12
	p.TimeBudget = 0
	p.Threads = 1
	p.Book = nil

	m, ok := p.pick(g)
	if !ok {
		t.Fatal("no move chosen")
	}
	t.Logf("VIv1K6hG move chosen by champion: %s at depth %d", m.UCI(), LastSearchDepth())
	if m.UCI() == "g4e3" {
		t.Errorf("champion blundered greedy fork 15... Ne3 (%s); should prefer defense like e7g5", m.UCI())
	}
}
