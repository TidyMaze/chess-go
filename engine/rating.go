package engine

import (
	"math"
	"runtime"
	"sync"
)

// Rating fitting over a round-robin, rather than chaining one match per
// rung.
//
// A chain breaks whenever any single link saturates: if engine B beats
// engine A 29-1, the score is at the edge of the logistic curve, the Elo
// estimate there is dominated by one game, and every rung above inherits
// that error. Some links genuinely are unmeasurable that way -- adding
// even 1-ply quiescence to a 1-ply search wins ~97% of games, and no
// amount of games fixes a 97% score.
//
// A round-robin instead measures every pair, and the ratings are fitted
// to *all* results at once by maximum likelihood. Saturated pairs simply
// contribute little information while the informative pairs pin the
// scale, so one lopsided matchup can no longer distort everything above
// it.

type pairResult struct {
	i, j  int
	score float64 // i's score share against j
	games int
}

// FitRatings runs a full round-robin and returns a rating per player,
// with players[anchor] pinned to 0 Elo.
func FitRatings(players []Player, gamesPerPair, maxMoves, anchor int) []float64 {
	var results []pairResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))

	for i := 0; i < len(players); i++ {
		for j := i + 1; j < len(players); j++ {
			wg.Add(1)
			go func(i, j int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				res := PlayMatch(players[i], players[j], gamesPerPair, maxMoves)
				mu.Lock()
				results = append(results, pairResult{i, j, res.Score(), res.Games()})
				mu.Unlock()
			}(i, j)
		}
	}
	wg.Wait()

	ratings := make([]float64, len(players))
	const lr = 8.0
	for iter := 0; iter < 20000; iter++ {
		grad := make([]float64, len(players))
		for _, r := range results {
			expected := 1.0 / (1.0 + math.Pow(10, (ratings[r.j]-ratings[r.i])/400))
			// Gradient of the log-likelihood w.r.t. rating difference.
			delta := float64(r.games) * (r.score - expected)
			grad[r.i] += delta
			grad[r.j] -= delta
		}
		for i := range ratings {
			ratings[i] += lr * grad[i] / 100
		}
		// Re-anchor every iteration so the scale stays pinned.
		shift := ratings[anchor]
		for i := range ratings {
			ratings[i] -= shift
		}
	}
	return ratings
}
