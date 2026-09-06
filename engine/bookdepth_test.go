package engine

import (
	"testing"

	"chess/game"
)

// How far does the book actually take the engine from the start? If it
// runs out after two plies it cannot be worth measurable Elo, and a match
// measuring it would be measuring noise.
func TestBookDepthFromTheStart(t *testing.T) {
	b, err := LoadBook("../book.txt")
	if err != nil {
		t.Skip("no book")
	}
	g := game.New()
	moves := []string{}
	for ply := 0; ply < 40; ply++ {
		m, ok := b.Move(g)
		if !ok {
			break
		}
		moves = append(moves, m.UCI())
		g.ApplyMove(m.From, m.To)
	}
	t.Logf("book covers %d plies from the initial position: %v", len(moves), moves)

	// And how often does it fire from a random real opening?
	book, err := LoadOpeningsList("../openings.txt")
	if err != nil {
		t.Skip("no openings")
	}
	hits, total, depths := 0, 0, 0
	for i := 0; i < 400 && i < len(book); i++ {
		g, err := game.ParseFEN(book[i])
		if err != nil {
			continue
		}
		total++
		d := 0
		for ply := 0; ply < 30; ply++ {
			m, ok := b.Move(g)
			if !ok {
				break
			}
			g.ApplyMove(m.From, m.To)
			d++
		}
		if d > 0 {
			hits++
		}
		depths += d
	}
	t.Logf("from %d real openings: %d hit the book (%.0f%%), average %.1f plies of book",
		total, hits, 100*float64(hits)/float64(total), float64(depths)/float64(total))
}
