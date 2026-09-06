package main

import (
	"math"
	"math/rand"
	"os"
	"testing"
)

// Idea 2 of the plateau campaign, measured directly.
//
// The claim under test: self-play data generated at playing depth from
// real opening positions is better training data than the current method,
// ten uniformly random plies played at depth 2. The two pools were
// generated at matched volume, 49,127 positions against 48,622, so the
// only difference is how the positions were reached and how deeply they
// were played.
//
// The judge is a neutral set drawn from the Lichess dump: real positions
// from real games, labelled by a depth-46 search. That is as close to the
// distribution the engine is actually used in as anything available, and
// neither arm was trained on it.
//
// Run with:
//
//	go test ./nnue/ -run TestTrainingDataQuality -v -timeout 30m
func TestTrainingDataQuality(t *testing.T) {
	for _, p := range []string{"../book_pool.bin", "../rand_pool.bin", "../lichess_pool.bin"} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("missing %s", p)
		}
	}

	judge, err := loadPool("../lichess_pool.bin", 50000)
	if err != nil {
		t.Fatal(err)
	}

	// What a constant prediction scores on the judge set, and what the
	// hand-written evaluation scores. Both arms have to be read against
	// these, not against each other alone.
	mean := 0.0
	for i := range judge {
		mean += judge[i].target
	}
	mean /= float64(len(judge))
	baseline, handMSE := 0.0, 0.0
	for i := range judge {
		d := judge[i].target - mean
		baseline += d * d
		h := judge[i].static - judge[i].target
		handMSE += h * h
	}
	baseline /= float64(len(judge))
	handMSE /= float64(len(judge))
	t.Logf("judge set: %d positions, constant predictor %.3f, hand evaluation %.3f",
		len(judge), baseline, handMSE)

	type arm struct {
		name string
		path string
	}
	results := map[string]float64{}
	for _, a := range []arm{
		{"book openings, depth 5", "../book_pool.bin"},
		{"random plies, depth 2", "../rand_pool.bin"},
	} {
		pool, err := loadPool(a.path, 0)
		if err != nil {
			t.Fatal(err)
		}
		rng := rand.New(rand.NewSource(23))
		n := newNet(32, rng)
		trainIdx, _ := splitByGame(pool, rng, 0)
		if len(trainIdx) == 0 {
			// splitByGame always holds something out; with 0% asked for it
			// still keeps one game, which is what we want here since the
			// judge set is external.
			t.Fatalf("%s: no training positions", a.name)
		}
		for e := 0; e < 30; e++ {
			rng.Shuffle(len(trainIdx), func(i, j int) {
				trainIdx[i], trainIdx[j] = trainIdx[j], trainIdx[i]
			})
			n.trainEpoch(pool, trainIdx, 0.001, 1e-4, 0.30, false, 10)
			n.smooth(0.05)
		}
		mse := n.loss(judge, 0.30, false)
		results[a.name] = mse
		explained := 100 * (1 - mse/baseline)
		t.Logf("%-24s trained on %6d positions -> judge MSE %.3f, explains %.1f%%",
			a.name, len(pool), mse, explained)
	}

	book := results["book openings, depth 5"]
	rnd := results["random plies, depth 2"]
	t.Logf("book data is %.1f%% %s on the playing distribution",
		100*math.Abs(book-rnd)/rnd,
		map[bool]string{true: "better", false: "worse"}[book < rnd])
	if book >= rnd {
		t.Logf("IDEA 2 NOT SUPPORTED: generating at playing depth from real openings "+
			"did not produce better training data (%.3f against %.3f)", book, rnd)
	}
}
