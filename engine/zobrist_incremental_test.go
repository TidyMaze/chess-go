package engine

import (
	"chess/board"
	"chess/game"
	"math/rand"
	"testing"
)

func TestZobristIncrementalMatchesFull(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for gameIdx := 0; gameIdx < 100; gameIdx++ {
		g := game.New()
		color := board.White
		key := zobristBoard(&g.Board, color)

		for ply := 0; ply < 80; ply++ {
			legal := g.AllLegalMoves(color)
			if len(legal) == 0 {
				break
			}
			m := legal[rng.Intn(len(legal))]
			undo, promoted := makeSearchMove(g, m)
			nextColor := color.Other()
			newKey := zobristUpdate(key, &g.Board, m, undo, promoted)
			expectedKey := zobristBoard(&g.Board, nextColor)

			if newKey != expectedKey {
				t.Fatalf("mismatch at game %d, ply %d, move %v: got %x, expected %x",
					gameIdx, ply, m, newKey, expectedKey)
			}

			key = newKey
			color = nextColor
		}
	}
}
