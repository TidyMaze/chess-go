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
	acc  string // EvaluateWith, the accumulator path the search uses
}{
	{"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", "0.0029633992817252874", "0.002964301034808159"},
	{"r1bqkbnr/pppp1ppp/2n5/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R b KQkq - 3 3", "0.013463207520544529", "0.013463577255606651"},
	{"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9", "0.10653994232416153", "0.10654006898403168"},
	{"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1", "0.7134010791778564", "0.7134011387825012"},
	{"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1", "-0.09509079158306122", "-0.09509073197841644"},
	{"8/8/4k3/8/8/8/8/K6R w - - 0 1", "5.755947589874268", "5.755947589874268"},
	{"4k3/8/8/8/8/8/8/4K2R w K - 0 1", "6.111911773681641", "6.111911773681641"},
}

func TestChampionNetworkStillEvaluatesToTheBit(t *testing.T) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network here:", err)
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
