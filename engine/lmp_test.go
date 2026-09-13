package engine

import (
	"testing"

	"chess/game"
)

// Late move pruning: past a move count that grows with depth, quiet moves
// are not worth searching at all. The rule is a pure function so every
// branch can be exercised here; the search consults it and nothing else.
func TestLateMovePruningThresholds(t *testing.T) {
	// (3 + depth^2) / 2 when not improving: depth 1 -> 2, 2 -> 3, 3 -> 6,
	// 4 -> 9, 5 -> 14. Move indexes are zero-based.
	for _, c := range []struct {
		depth, index int
		want         bool
	}{
		{1, 1, false}, {1, 2, true},
		{2, 2, false}, {2, 3, true},
		{3, 5, false}, {3, 6, true},
		{4, 8, false}, {4, 9, true},
		{5, 13, false}, {5, 14, true},
		{6, 100, false}, // beyond the depth cap, never
	} {
		got := lateMovePruned(c.depth, c.index, false, false, false, false, false, false)
		if got != c.want {
			t.Errorf("depth %d move %d: pruned %v, want %v", c.depth, c.index, got, c.want)
		}
	}
	// Improving raises the threshold: (3 + d^2) / 1.
	if lateMovePruned(3, 6, true, false, false, false, false, false) {
		t.Error("improving at depth 3, move 6 should not be pruned (threshold 12)")
	}
	if !lateMovePruned(3, 12, true, false, false, false, false, false) {
		t.Error("improving at depth 3, move 12 should be pruned")
	}
}

// The moves that must never be late-move pruned, whatever the count.
func TestLateMovePruningExemptions(t *testing.T) {
	const depth, late = 2, 40
	base := lateMovePruned(depth, late, false, false, false, false, false, false)
	if !base {
		t.Fatal("the control case must prune")
	}
	cases := map[string][5]bool{ // inCheck, isCapture, promoted, givesCheck, isKiller
		"in check":    {true, false, false, false, false},
		"capture":     {false, true, false, false, false},
		"promotion":   {false, false, true, false, false},
		"gives check": {false, false, false, true, false},
		"killer":      {false, false, false, false, true},
	}
	for name, f := range cases {
		if lateMovePruned(depth, late, false, f[0], f[1], f[2], f[3], f[4]) {
			t.Errorf("a %s move was late-move pruned", name)
		}
	}
}

func TestDeepLateMovePruning(t *testing.T) {
	// With maxDepth = 8:
	// depth 6: (3 + 36)/2 = 19
	// depth 7: (3 + 49)/2 = 26
	// depth 8: (3 + 64)/2 = 33
	// depth 9: beyond cap
	for _, c := range []struct {
		depth, index int
		want         bool
	}{
		{6, 18, false}, {6, 19, true},
		{7, 25, false}, {7, 26, true},
		{8, 32, false}, {8, 33, true},
		{9, 100, false},
	} {
		got := lateMovePrunedMax(c.depth, c.index, false, false, false, false, false, false, 8)
		if got != c.want {
			t.Errorf("depth %d move %d maxDepth 8: pruned %v, want %v", c.depth, c.index, got, c.want)
		}
	}
}


// Wired in, the rule must actually remove nodes.
func TestLateMovePruningCutsNodes(t *testing.T) {
	var with, without int
	for _, fen := range correctnessPositions[:6] {
		g, _ := game.ParseFEN(fen)
		off, on := Strong(5), Strong(5)
		on.LMP = true
		ResetNodes()
		PlayerScoreWith(off, g, nil)
		without += TotalNodes()
		ResetNodes()
		PlayerScoreWith(on, g, nil)
		with += TotalNodes()
	}
	t.Logf("nodes: %d without late move pruning, %d with (%.2fx)", without, with, float64(without)/float64(with))
	if with >= without {
		t.Errorf("late move pruning removed no nodes: %d with, %d without", with, without)
	}
}

func TestDeepLateMovePruningCutsNodes(t *testing.T) {
	var with, without int
	for _, fen := range correctnessPositions[:4] {
		g, _ := game.ParseFEN(fen)
		off, on := Strong(7), Strong(7)
		off.LMP = true
		off.DeepLMP = false
		on.LMP = true
		on.DeepLMP = true
		ResetNodes()
		PlayerScoreWith(off, g, nil)
		without += TotalNodes()
		ResetNodes()
		PlayerScoreWith(on, g, nil)
		with += TotalNodes()
	}
	t.Logf("nodes at depth 7: %d standard LMP, %d deep LMP (%.2fx)", without, with, float64(without)/float64(with))
	if with >= without {
		t.Errorf("deep late move pruning removed no nodes: %d with, %d without", with, without)
	}
}

