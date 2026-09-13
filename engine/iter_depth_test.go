package engine

import (
	"chess/game"
	"testing"
	"time"
)

func TestIterativeDeepeningProgression(t *testing.T) {
	fen := "r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9"
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}
	for d := 1; d <= 14; d++ {
		p := Strong(d)
		p.ApplyFeatures("lmp,deeplmp,rfp,scaledlmr,countermove,see,historymalus,lmrtwostep,nullgate")
		ResetNodes()
		t0 := time.Now()
		m, ok := PlayerPick(p, g)
		elapsed := time.Since(t0)
		nodes := TotalNodes()
		t.Logf("depth %2d: move=%v ok=%v elapsed=%10v nodes=%8d knps=%6.0f",
			d, m, ok, elapsed, nodes, float64(nodes)/elapsed.Seconds()/1000)
		if elapsed > 2*time.Second {
			break
		}
	}
}
