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

// PairScore is a match result in the form FitFromResults consumes, so a
// caller running its own matches (e.g. to stream progress to a UI) can
// still use the same rating fit.
type PairScore struct {
	I, J  int
	Score float64
	Games int
}

// FitFromResults fits ratings to already-played match results, with
// players[anchor] pinned to 0 Elo.
func FitFromResults(numPlayers int, scores []PairScore, anchor int) []float64 {
	results := make([]pairResult, len(scores))
	for i, s := range scores {
		results[i] = pairResult{s.I, s.J, s.Score, s.Games}
	}
	return fitRatings(numPlayers, results, anchor)
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

	return fitRatings(len(players), results, anchor)
}

func fitRatings(numPlayers int, results []pairResult, anchor int) []float64 {
	ratings := make([]float64, numPlayers)
	const lr = 8.0
	for iter := 0; iter < 20000; iter++ {
		grad := make([]float64, numPlayers)
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
