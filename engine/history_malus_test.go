package engine

import (
	"testing"

	"chess/game"
)

func TestHistoryMalusPenalizesFailedQuiets(t *testing.T) {
	g, err := game.ParseFEN(correctnessPositions[0])
	if err != nil {
		t.Fatal(err)
	}

	pOff := Strong(5)
	pOff.HistoryMalus = false

	pOn := Strong(5)
	pOn.HistoryMalus = true

	// Both should choose a valid legal move
	mOff, ok1 := PlayerPick(pOff, g)
	mOn, ok2 := PlayerPick(pOn, g)
	if !ok1 || !ok2 {
		t.Fatalf("failed to pick move: off=%v, on=%v", ok1, ok2)
	}
	t.Logf("move off: %v, move on: %v", mOff.UCI(), mOn.UCI())

	// Ordering stats with HistoryMalus
	ResetOrderingStats()
	OrderingStats = true
	defer func() { OrderingStats = false }()

	PlayerScoreWith(pOn, g, nil)
	rate, _, total := OrderingReport()
	if total == 0 || rate <= 0 {
		t.Errorf("history malus failed to record cutoffs: total=%d, rate=%v", total, rate)
	}
}

func TestHistoryMalusFirstMoveRate(t *testing.T) {
	measure := func(on bool) (float64, int64) {
		ResetOrderingStats()
		OrderingStats = true
		defer func() { OrderingStats = false }()
		var totalNodes int64
		for _, fen := range correctnessPositions[:6] {
			g, _ := game.ParseFEN(fen)
			p := Strong(5)
			p.HistoryMalus = on
			ResetNodes()
			PlayerScoreWith(p, g, nil)
			totalNodes += int64(TotalNodes())
		}
		rate, _, _ := OrderingReport()
		return rate, totalNodes
	}

	rateOff, nodesOff := measure(false)
	rateOn, nodesOn := measure(true)
	t.Logf("cutoff rate: off=%.2f%% on=%.2f%%; nodes: off=%d on=%d", rateOff*100, rateOn*100, nodesOff, nodesOn)
	if nodesOn > nodesOff*11/10 {
		t.Errorf("history malus caused >10%% node bloat: off=%d, on=%d", nodesOff, nodesOn)
	}
}
