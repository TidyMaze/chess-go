package engine

import (
	"testing"

	"chess/board"
)

// How much of probe's cost is the code and how much is the memory it
// touches: a warm probe of a small table against random probes of the
// full 2^20 table the engine plays with.
func BenchmarkTTProbeWarm(b *testing.B) {
	t := NewTranspositionTable(10)
	t.store(12345, 0.5, 3, 0, ttExact, board.White)
	for i := 0; i < b.N; i++ {
		t.probe(12345, 3, 0, 0, board.White, -1, 1)
	}
}

func BenchmarkTTProbeRandomLarge(b *testing.B) {
	t := NewTranspositionTable(20)
	var x uint64 = 88172645463325252
	for i := 0; i < b.N; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		t.probe(x, 3, 0, 0, board.White, -1, 1)
	}
}
