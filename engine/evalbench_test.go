package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// What does the network actually cost inside the search, against the
// hand-written evaluation it replaces? This is the number that decides
// whether inference is worth optimising, and it is not the same question
// as how fast training is.
func BenchmarkEvalHand(b *testing.B) {
	g, _ := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	ev := &Eval{Weights: DefaultWeights(), UsePST: true, Tapered: true,
		Structure: true, Mobility: true, KingSafety: 0.01}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PositionScoreEval(&g.Board, board.White, ev)
	}
}

func BenchmarkEvalHalfKP(b *testing.B) {
	n, err := LoadHalfKPNet("/tmp/big_net_256.json")
	if err != nil {
		b.Skip("no network available")
	}
	g, _ := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
	ev := &Eval{Weights: DefaultWeights(), UsePST: true, Tapered: true,
		Structure: true, Mobility: true, KingSafety: 0.01, HalfKP: n, HalfKPBlend: 0.45}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PositionScoreEval(&g.Board, board.White, ev)
	}
}
