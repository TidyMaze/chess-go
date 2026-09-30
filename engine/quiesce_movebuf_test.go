package engine

import (
	"testing"

	"chess/game"
)

// Quiescence takes its move list storage from the search's per-slot stack
// instead of a 96-move array zeroed on every node. Each slot is its own
// storage, empty and able to hold 96 moves, and outside the search (no
// stack, or a slot past its end) there is none, so the caller falls back
// to its own array.
func TestQuiesceMoveBufIsTheSlotsOwnStorage(t *testing.T) {
	var stack [accSlots][96]game.Move
	ev := &Eval{qMoves: &stack}
	for _, slot := range []int{0, 1, accSlots - 1} {
		buf := ev.quiesceMoveBuf(slot)
		if len(buf) != 0 || cap(buf) != 96 {
			t.Fatalf("slot %d: len %d cap %d, want 0 and 96", slot, len(buf), cap(buf))
		}
		if &buf[:1][0] != &stack[slot][0] {
			t.Fatalf("slot %d: not the slot's own storage", slot)
		}
	}
	for _, slot := range []int{-1, accSlots} {
		if buf := ev.quiesceMoveBuf(slot); buf != nil {
			t.Fatalf("slot %d is past the stack, got a buffer", slot)
		}
	}
	if buf := (&Eval{}).quiesceMoveBuf(0); buf != nil {
		t.Fatal("no stack outside the search, got a buffer")
	}
	if buf := (*Eval)(nil).quiesceMoveBuf(0); buf != nil {
		t.Fatal("nil Eval, got a buffer")
	}
}
