package engine

import (
	"fmt"
	"math/rand"
	"testing"

	"chess/game"
)

// How often the champion's book actually fires once the harness has
// opened the game with random plies. An A/B that removes the book
// measures nothing if the book never speaks.
func TestBookHitRateAfterRandomOpening(t *testing.T) {
	book, err := LoadBook("../games_book_v5.txt")
	if err != nil {
		t.Skip("no book on disk")
	}
	r := rand.New(rand.NewSource(1))
	for _, plies := range []int{0, 6} {
		hits, asked := 0, 0
		for g := 0; g < 60; g++ {
			pos := game.New()
			for i := 0; i < plies; i++ {
				ms := pos.AllLegalMoves(pos.Turn)
				if len(ms) == 0 {
					break
				}
				m := ms[r.Intn(len(ms))]
				pos.ApplyMove(m.From, m.To)
			}
			// Ten plies of book questions after the random opening.
			for i := 0; i < 10; i++ {
				asked++
				m, ok := book.Move(pos)
				if !ok {
					break
				}
				hits++
				pos.ApplyMove(m.From, m.To)
			}
		}
		fmt.Printf("after %d random plies: %d book hits out of %d questions (%.1f%%)\n",
			plies, hits, asked, 100*float64(hits)/float64(asked))
	}
}
