package engine

import (
	"os"
	"strconv"
	"testing"

	"chess/game"
)

// A fixed workload for profiling: the champion's configuration, depth 6,
// four middlegame positions. Run with
//
//	PROFILE=1 go test ./engine/ -run TestProfileWorkload -cpuprofile cpu.out
func TestProfileWorkload(t *testing.T) {
	if os.Getenv("PROFILE") == "" {
		t.Skip("set PROFILE=1")
	}
	// Built by hand rather than from champion.json: its paths are relative
	// to the repository root and tests run in engine/.
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Fatal(err)
	}
	p := Strong(6)
	p.HalfKP, p.HalfKPBlend = net, 0.45
	// TTBITS overrides the table size, for measuring probe cost against
	// hit rate.
	if v := os.Getenv("TTBITS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatal(err)
		}
		p.TTBits = uint(n)
	}
	ResetNodes()
	for _, fen := range []string{
		"r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"r2q1rk1/1b1nbppp/p2ppn2/1p6/3NPP2/1BN1B3/PPPQ2PP/2KR3R w - - 0 12",
		"2rq1rk1/pp1bppbp/2np1np1/8/2BNP3/2N1BP2/PPPQ2PP/2KR3R w - - 0 11",
	} {
		g, _ := game.ParseFEN(fen)
		PlayerPick(p, g)
	}
	t.Logf("nodes: %d", TotalNodes())
}
