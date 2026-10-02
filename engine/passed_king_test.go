package engine

import (
	"chess/game"
	"testing"
)

func TestOutsidePassedPawnKingDoesNotStray(t *testing.T) {
	// 8eQ3xHrn move 53: White has outside passed pawn on h5 (with king on h4).
	// Black king is on e5.
	// Black king must NOT abandon kingside to wander to queenside (Kd6 -> Kc5 -> Kb3).
	// Black must keep king or bishop controlling the passed pawn (e.g. 53... Bh7 or 53... Ke6).
	g, err := game.ParseFEN("8/8/5p2/1p2kb1P/p4p1K/P4B2/1P4P1/8 b - - 2 53")
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
	p.ApplyFeatures("passedking")

	m, ok := p.pick(g)
	if !ok {
		t.Fatal("no move chosen")
	}
	t.Logf("chosen move: %s", m.UCI())
	if m.UCI() == "e5d6" || m.UCI() == "e5d4" {
		t.Errorf("Black king abandoned the outside passed pawn with %s", m.UCI())
	}
}
