package engine

import "testing"

// The training workload: play a move at depth 2, then label the position
// with a depth-4 search. This is where nearly all wall-clock time goes
// when generating training data, so it is what to optimise.
func BenchmarkTrainingWorkload(b *testing.B) {
	play := fullPlayer(2)
	label := fullPlayer(4)
	tt := NewTranspositionTable(16)
	for i := 0; i < b.N; i++ {
		g := midOpeningPosition()
		m, _ := PlayerPick(play, g)
		g.ApplyMove(m.From, m.To)
		PlayerScoreWith(label, g, tt)
	}
}
