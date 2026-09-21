package engine

import (
	"strconv"
	"testing"

	"chess/game"
)

// The shipped network's answers, recorded before the second hidden layer
// was added to the forward pass.
//
// Every measurement this project has made rests on one number per
// position, so a change to the output layers has to leave an existing
// file evaluating to the bit and not to a tolerance. A tolerance would
// hide exactly the kind of drift that makes two calibrations of the same
// network disagree, and the whole ladder is calibrated against this file.
//
// The values are printed with 'g' and -1 digits, which round-trips a
// float64 exactly, so comparing the strings compares the bits.
var championOutputs = []struct {
	fen  string
	full string // Evaluate, the standalone path
	// EvaluateWith, the accumulator path the search uses. Recorded equal to
	// full since the accumulator started applying rows in extraction order
	// rather than sorted; the earlier values differed in the last float32
	// bits only, and the two paths agreeing is the stronger invariant.
	acc string
}{
	{"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", "-0.04816409945487976", "-0.04816409945487976"},
	{"r1bqkbnr/pppp1ppp/2n5/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R b KQkq - 3 3", "0.265337735414505", "0.265337735414505"},
	{"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9", "0.19827191531658173", "0.19827191531658173"},
	{"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1", "0.807697057723999", "0.807697057723999"},
	{"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1", "0.7062534093856812", "0.7062534093856812"},
	{"8/8/4k3/8/8/8/8/K6R w - - 0 1", "5.391724109649658", "5.391724109649658"},
	{"4k3/8/8/8/8/8/8/4K2R w K - 0 1", "5.794571399688721", "5.794571399688721"},
}

func TestChampionNetworkStillEvaluatesToTheBit(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network here:", err)
	}
	if net.H2 != 0 {
		t.Fatalf("champion_net.json declares a second hidden layer of %d units, "+
			"so it is not the file this baseline was recorded from", net.H2)
	}
	for _, c := range championOutputs {
		g, err := game.ParseFEN(c.fen)
		if err != nil {
			t.Fatal(err)
		}
		full := strconv.FormatFloat(net.Evaluate(&g.Board), 'g', -1, 64)
		var slot halfKPAcc
		acc := strconv.FormatFloat(net.EvaluateWith(&g.Board, &slot, nil, nil), 'g', -1, 64)
		if full != c.full || acc != c.acc {
			t.Errorf("%s\n  Evaluate     %s, recorded %s\n  EvaluateWith %s, recorded %s",
				c.fen, full, c.full, acc, c.acc)
		}
	}
}
