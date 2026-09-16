package engine

import (
	"os"
	"strconv"
	"testing"

	"chess/board"
	"chess/game"
)

// The champion's hand evaluation, pinned to the bit on the correctness
// positions, both sides to move. The mobility and king-safety terms count
// squares and attackers; they were rewritten from generated move lists to
// bitboard population counts, which must leave every value untouched:
// a count is a count, whichever way it is taken. A tolerance here would
// let a wrong mask hide behind float noise.
//
// Recorded with EVAL_GOLDEN_RECORD=1 before that rewrite.
var handEvalGoldens = map[string][2]string{
	"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1":             {"-2.220446049250313e-16", "2.220446049250313e-16"},
	"rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1":          {"0.7249999999999999", "-0.7249999999999999"},
	"rnbqkbnr/ppp1pppp/8/3p4/4P3/8/PPPP1PPP/RNBQKBNR w KQkq d6 0 2":        {"0.02999999999999997", "-0.02999999999999997"},
	"r1bqkbnr/pppp1ppp/2n5/4p3/2B1P3/5N2/PPPP1PPP/RNBQK2R b KQkq - 3 3":    {"0.3220000000000001", "-0.3220000000000001"},
	"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9":     {"0.41200000000000014", "-0.41200000000000014"},
	"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1": {"1.2020000000000002", "-1.2020000000000002"},
	"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1":                            {"-0.5400000000000001", "0.5400000000000001"},
	"8/8/4k3/8/8/8/8/K6R w - - 0 1":                                        {"5.075", "-5.075"},
	"4k3/8/8/8/8/8/8/4K2R b K - 0 1":                                       {"5.7749999999999995", "-5.7749999999999995"},
	"6k1/5ppp/8/8/8/8/5PPP/6K1 w - - 0 1":                                  {"5.551115123125783e-17", "-5.551115123125783e-17"},
}

func TestChampionHandEvaluationIsUnchanged(t *testing.T) {
	wd, _ := os.Getwd()
	if err := os.Chdir(".."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	p := ReadChampion("champion_bot.json").Player()
	if p.HalfKP == nil {
		t.Skip("champion network did not load")
	}
	// The network is pinned elsewhere; this test is about the hand terms.
	p.HalfKP, p.Net = nil, nil
	ev := evalForPlayer(p)
	record := os.Getenv("EVAL_GOLDEN_RECORD") != ""
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		var got [2]string
		for i, side := range []board.Color{board.White, board.Black} {
			got[i] = strconv.FormatFloat(PositionScoreEval(&g.Board, side, ev), 'g', -1, 64)
		}
		if record {
			t.Logf("\t%q: {%q, %q},", fen, got[0], got[1])
			continue
		}
		want, ok := handEvalGoldens[fen]
		if !ok {
			t.Errorf("no golden recorded for %s", fen)
			continue
		}
		if got != want {
			t.Errorf("%s\n  got  %v\n  want %v", fen, got, want)
		}
	}
}
