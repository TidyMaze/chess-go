package main

import (
	"math/rand"
	"testing"
)

// The by-game split is the guard against the most expensive measurement
// error this project has made. Held-out positions were once drawn at
// random from the same pool as training positions; consecutive positions
// in a game differ by one move, so every held-out position had a near
// copy in training. That network reported held-out error 1.36 against the
// hand evaluation's 5.89, called itself four times better, and lost
// 0-0-60. Splitting by game turned the same number into 2.7x worse.
//
// So: no game identity may appear on both sides of the split, ever.
func TestSplitByGameNeverLeaksAGame(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	var pool []sample
	for g := 0; g < 200; g++ {
		for p := 0; p < 8; p++ {
			pool = append(pool, sample{game: int32(g), target: float64(g)})
		}
	}
	trainIdx, testSet := splitByGame(pool, rng, 15)
	if len(trainIdx) == 0 || len(testSet) == 0 {
		t.Fatalf("split gave %d training and %d held out", len(trainIdx), len(testSet))
	}
	trainGames := map[int32]bool{}
	for _, i := range trainIdx {
		trainGames[pool[i].game] = true
	}
	for _, s := range testSet {
		if trainGames[s.game] {
			t.Fatalf("game %d appears in both training and held-out sets", s.game)
		}
	}
	if len(trainIdx)+len(testSet) != len(pool) {
		t.Errorf("split covers %d positions, pool has %d",
			len(trainIdx)+len(testSet), len(pool))
	}
}

// An empty pool is what a run with no self-play and an unloaded pool file
// looks like. It must report as empty rather than panic on a slice bound,
// because the panic gives no clue that the pool simply failed to load.
func TestSplitByGameHandlesAnEmptyPool(t *testing.T) {
	trainIdx, testSet := splitByGame(nil, rand.New(rand.NewSource(1)), 15)
	if len(trainIdx) != 0 || len(testSet) != 0 {
		t.Errorf("empty pool gave %d training and %d held out", len(trainIdx), len(testSet))
	}
}

// One game cannot be split, and asking for a held-out set from it must not
// leave the training side empty.
func TestSplitByGameWithASingleGame(t *testing.T) {
	pool := []sample{{game: 1}, {game: 1}, {game: 1}}
	trainIdx, testSet := splitByGame(pool, rand.New(rand.NewSource(1)), 15)
	if len(trainIdx)+len(testSet) != len(pool) {
		t.Errorf("split covers %d, want %d", len(trainIdx)+len(testSet), len(pool))
	}
}
