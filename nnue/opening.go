package main

import (
	"math/rand"

	"chess/game"
)

// startingPosition decides where a self-play game begins.
//
// With no book it plays ten uniformly random plies from the initial
// position, which is what this pipeline has always done and which is idea
// 2's target. Measured, that produces a population the engine never meets
// in play: squared error against a depth-6 reference was 1.57 on those
// positions and 2.80 on positions from real play, and the material
// profile differed as sharply, mean gap 3.47 against 6.79 and 22%
// lopsided against 53%.
//
// With a book it starts from a position taken from a real analysed game,
// so the self-play that follows explores from somewhere a game actually
// reaches.
//
// A book entry that fails to parse falls back to the random opening
// rather than aborting the game: one bad line in a book of a hundred
// thousand must not cost a generation.
func startingPosition(book []string, rng *rand.Rand) *game.Game {
	if len(book) > 0 {
		if g, err := game.ParseFEN(book[rng.Intn(len(book))]); err == nil {
			return g
		}
	}
	return randomOpeningGame(rng, 10)
}

func randomOpeningGame(rng *rand.Rand, plies int) *game.Game {
	g := game.New()
	for i := 0; i < plies; i++ {
		legal := g.AllLegalMoves(g.Turn)
		if len(legal) == 0 {
			break
		}
		m := legal[rng.Intn(len(legal))]
		g.ApplyMove(m.From, m.To)
	}
	return g
}
