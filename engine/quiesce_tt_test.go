package engine

import (
	"chess/board"
	"chess/game"
	"sync/atomic"
	"testing"
)

func TestQuiescenceTTProbing(t *testing.T) {
	// A tactical position with multiple capture sequences leading to the same leaf
	fen := "r1bqkb1r/pppp1ppp/2n5/4p3/2B1n3/5N2/PPPP1PPP/RNBQK2R w KQkq - 0 5"
	g, err := game.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	evWithoutTT := &Eval{}
	atomic.StoreInt64(&quiesceNodes, 0)
	score1 := quiesce(g, board.White, board.White, -100, 100, evWithoutTT, 0, 0)
	nodesWithoutTT := atomic.LoadInt64(&quiesceNodes)

	evWithTT := &Eval{Table: NewTranspositionTable(16)}
	atomic.StoreInt64(&quiesceNodes, 0)
	// Run twice to ensure table warms up and cuts off
	score2 := quiesce(g, board.White, board.White, -100, 100, evWithTT, 0, 0)
	atomic.StoreInt64(&quiesceNodes, 0)
	score3 := quiesce(g, board.White, board.White, -100, 100, evWithTT, 0, 0)
	nodesSecondRun := atomic.LoadInt64(&quiesceNodes)

	if score1 != score2 || score2 != score3 {
		t.Errorf("scores differ: %f vs %f vs %f", score1, score2, score3)
	}
	if nodesSecondRun != 1 {
		t.Errorf("expected 1 node on cached TT hit, got %d (without TT: %d)", nodesSecondRun, nodesWithoutTT)
	}
}
