package engine

import "testing"

func TestReportNodesDepth6(t *testing.T) {
	p := fullPlayer(6)
	g := midOpeningPosition()
	ResetNodes()
	PlayerPick(p, g)
	t.Logf("depth 6 nodes: %d", TotalNodes())
}
