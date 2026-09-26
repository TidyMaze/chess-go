package engine

import (
	"chess/board"
	"chess/game"
	"testing"
)

func TestProbeWithMoveExact(t *testing.T) {
	tt := NewTranspositionTable(8)
	key := uint64(0x123456789abcdef0)
	m := game.Move{From: board.Sq{File: 1, Rank: 1}, To: board.Sq{File: 1, Rank: 3}}
	tt.storeWithMove(key, 1.5, 4, 0, ttExact, board.White, m)

	score, cutoff, move, okMove := tt.probeWithMove(key, 4, 0, board.White, -2.0, 2.0)
	if !cutoff || score != 1.5 {
		t.Fatalf("expected cutoff with score 1.5, got cutoff=%v score=%v", cutoff, score)
	}
	if !okMove || move != m {
		t.Fatalf("expected move %v, got okMove=%v move=%v", m, okMove, move)
	}
}

func TestProbeWithMoveNoCutoffReturnsMove(t *testing.T) {
	tt := NewTranspositionTable(8)
	key := uint64(0x123456789abcdef0)
	m := game.Move{From: board.Sq{File: 1, Rank: 1}, To: board.Sq{File: 1, Rank: 3}}
	// Stored with depth 2, probing at depth 4
	tt.storeWithMove(key, 1.5, 2, 0, ttExact, board.White, m)

	_, cutoff, move, okMove := tt.probeWithMove(key, 4, 0, board.White, -2.0, 2.0)
	if cutoff {
		t.Fatalf("expected no cutoff for shallow depth")
	}
	if !okMove || move != m {
		t.Fatalf("expected move %v, got okMove=%v move=%v", m, okMove, move)
	}
}

func TestProbeWithMoveMiss(t *testing.T) {
	tt := NewTranspositionTable(8)
	key := uint64(0x123456789abcdef0)

	_, cutoff, _, okMove := tt.probeWithMove(key, 4, 0, board.White, -2.0, 2.0)
	if cutoff || okMove {
		t.Fatalf("expected miss on empty table, got cutoff=%v okMove=%v", cutoff, okMove)
	}
}
