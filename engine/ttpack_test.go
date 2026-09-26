package engine

import (
	"testing"

	"chess/board"
	"chess/game"
)

// Every square pair, flag and side survives the packing: the move ordering
// and the cutoffs read exactly what was stored.
func TestTTEntryPackingRoundTrips(t *testing.T) {
	for from := uint8(0); from < 64; from++ {
		for to := uint8(0); to < 64; to++ {
			for _, flag := range []ttFlag{ttExact, ttLowerBound, ttUpperBound} {
				for side := uint8(0); side < 2; side++ {
					e := ttEntry{data: packTTData(from, to, flag, side)}
					if e.from() != from || e.to() != to || e.flag() != flag || e.maximizingFor() != side {
						t.Fatalf("packed (%d %d %d %d), read back (%d %d %d %d)",
							from, to, flag, side, e.from(), e.to(), e.flag(), e.maximizingFor())
					}
				}
			}
		}
	}
}

// prefetch is a hint: on a nil table it does nothing, on any key it neither
// panics nor changes what a probe reads.
func TestTTPrefetchLeavesTheTableAlone(t *testing.T) {
	var none *TranspositionTable
	none.prefetch(0x1234)

	tt := NewTranspositionTable(4)
	key := uint64(0xabcdef0123456789)
	m := game.Move{From: board.Sq{File: 4, Rank: 1}, To: board.Sq{File: 4, Rank: 3}}
	tt.storeWithMove(key, 0.5, 3, 0, ttLowerBound, board.Black, m)
	for k := uint64(0); k < 64; k++ {
		tt.prefetch(k)
	}
	tt.prefetch(^uint64(0))
	tt.prefetch(key)
	score, cutoff, got, okMove := tt.probeWithMove(key, 3, 0, 0, board.Black, 0, 0.4)
	if !cutoff || score != 0.5 || !okMove || got != m {
		t.Fatalf("after prefetch: probeWithMove = %v %v %v %v, want 0.5 true %v true", score, cutoff, got, okMove, m)
	}
}

// An empty slot must read as the old zero entry did: exact, White, a1a1.
// A key whose upper half is zero matches it, and returns that move.
func TestTTEmptySlotReadsAsZeroEntry(t *testing.T) {
	if got := packTTData(0, 0, ttExact, uint8(board.White)); got != 0 {
		t.Fatalf("packTTData of the zero fields = %#x, want 0", got)
	}
	tt := NewTranspositionTable(4)
	m, ok := tt.bestMove(0x5)
	if !ok || m != (game.Move{}) {
		t.Fatalf("empty slot, key upper half 0: bestMove = %v %v, want a1a1 true", m, ok)
	}
}
