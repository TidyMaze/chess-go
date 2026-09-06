package main

import (
	"math/rand"
	"sort"
)

// splitByGame divides the pool into training indices and a held-out set,
// splitting on game identity rather than on position.
//
// This is not a style preference. Consecutive positions within a game
// differ by a single move, so a split that shuffles positions puts near
// copies of held-out positions into training and reports a score the
// network cannot reproduce in play. Measured: the same network scored
// held-out error 1.36 against the hand evaluation's 5.89 under a position
// split, and 10.05 against 3.78 under a game split. The first number said
// four times better and the network lost sixty games out of sixty.
//
// heldOutPct is the share of games, not positions, put aside.
func splitByGame(pool []sample, rng *rand.Rand, heldOutPct int) (trainIdx []int32, testSet []sample) {
	if len(pool) == 0 {
		return nil, nil
	}

	ids := map[int32]bool{}
	for i := range pool {
		ids[pool[i].game] = true
	}
	idList := make([]int32, 0, len(ids))
	for id := range ids {
		idList = append(idList, id)
	}
	// Sorted before shuffling so the split depends on the seed and not on
	// Go's map iteration order, which would make a resumed run hold out a
	// different set from the run it resumed.
	sort.Slice(idList, func(i, j int) bool { return idList[i] < idList[j] })
	rng.Shuffle(len(idList), func(i, j int) { idList[i], idList[j] = idList[j], idList[i] })

	n := 1 + len(idList)*heldOutPct/100
	if n > len(idList) {
		n = len(idList)
	}
	heldOut := make(map[int32]bool, n)
	for _, id := range idList[:n] {
		heldOut[id] = true
	}

	for i := range pool {
		if heldOut[pool[i].game] {
			testSet = append(testSet, pool[i])
		} else {
			trainIdx = append(trainIdx, int32(i))
		}
	}
	return trainIdx, testSet
}

// constantBaseline is the squared error of predicting one number, the
// mean, for every position.
//
// It is the only honest reference for "is this network learning". A
// held-out loss compared against the hand-written evaluation is not: on
// the Lichess pool a network scored 9.72 against the hand evaluation's
// 13.5, read as "more accurate", and was worse than this baseline of
// 9.80. It had learned 0.8% of the variance and its labels had a sign
// bug, and nothing else being printed could show that.
func constantBaseline(data []sample) float64 {
	if len(data) == 0 {
		return 0
	}
	mean := 0.0
	for i := range data {
		mean += data[i].target
	}
	mean /= float64(len(data))
	mse := 0.0
	for i := range data {
		d := data[i].target - mean
		mse += d * d
	}
	return mse / float64(len(data))
}
