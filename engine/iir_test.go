package engine

import (
	"testing"

	"chess/game"
)

func TestInternalIterativeReductionRule(t *testing.T) {
	if !iirReduces(6, false) || !iirReduces(9, false) {
		t.Error("depth >= 6 without a table move must reduce")
	}
	if iirReduces(5, false) {
		t.Error("depth 5 must not reduce")
	}
	if iirReduces(8, true) {
		t.Error("a node with a table move must not reduce")
	}
}

func TestInternalIterativeReductionCutsNodes(t *testing.T) {
	var with, without int
	for _, fen := range correctnessPositions[:6] {
		g, _ := game.ParseFEN(fen)
		off, on := Strong(7), Strong(7)
		on.IIR = true
		ResetNodes()
		PlayerScoreWith(off, g, nil)
		without += TotalNodes()
		ResetNodes()
		PlayerScoreWith(on, g, nil)
		with += TotalNodes()
	}
	t.Logf("nodes at depth 7: %d without IIR, %d with (%.2fx)", without, with, float64(without)/float64(with))
	if with >= without {
		t.Errorf("IIR removed no nodes: %d with, %d without", with, without)
	}
}
