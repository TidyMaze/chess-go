package engine

import "testing"

// Depth 6 and 7 are where the engine is being asked to operate now, and
// the cost profile there is not the same as at depth 3: move ordering
// and the transposition table matter far more when the tree is 50x
// bigger, and per-node costs that were invisible become the whole bill.
func fullPlayer(d int) Player {
	return Player{Depth: d, UsePST: true, Quiescence: true, TTBits: 20,
		NullMove: true, Tapered: true, Iterative: true, Extensions: true,
		Aspiration: true, SEEPruning: true, Structure: true, Futility: true}
}

func BenchmarkDepth6(b *testing.B) {
	p := fullPlayer(6)
	for i := 0; i < b.N; i++ {
		g := midOpeningPosition()
		PlayerPick(p, g)
	}
}

func BenchmarkDepth7(b *testing.B) {
	p := fullPlayer(7)
	for i := 0; i < b.N; i++ {
		g := midOpeningPosition()
		PlayerPick(p, g)
	}
}
