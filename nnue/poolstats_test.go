package main

import (
	"math"
	"os"
	"testing"
)

// A diagnostic over a real pool file, skipped when there is none.
//
// It exists because two numbers from the Lichess training run could not
// both be true: the hand-written evaluation scored 13.5 against a
// constant predictor's 9.8 on the same positions. The hand evaluation
// counts material, and material explains most of the variance in a set of
// chess positions, so being beaten by a single constant means something
// about the pool is wrong rather than something about the evaluation.
func TestPoolIsSane(t *testing.T) {
	const path = "../lichess_pool.bin"
	if _, err := os.Stat(path); err != nil {
		t.Skip("no pool file to inspect")
	}
	pool, err := loadPool(path, 200000)
	if err != nil {
		t.Fatal(err)
	}
	if len(pool) < 1000 {
		t.Skipf("pool has only %d positions", len(pool))
	}

	stats := func(get func(sample) float64) (mean, sd float64) {
		for i := range pool {
			mean += get(pool[i])
		}
		mean /= float64(len(pool))
		for i := range pool {
			d := get(pool[i]) - mean
			sd += d * d
		}
		return mean, math.Sqrt(sd / float64(len(pool)))
	}
	tMean, tSD := stats(func(s sample) float64 { return s.target })
	sMean, sSD := stats(func(s sample) float64 { return s.static })

	cov, handMSE := 0.0, 0.0
	for i := range pool {
		cov += (pool[i].target - tMean) * (pool[i].static - sMean)
		d := pool[i].static - pool[i].target
		handMSE += d * d
	}
	cov /= float64(len(pool))
	handMSE /= float64(len(pool))
	corr := cov / (tSD * sSD)

	var feats, empty int
	for i := range pool {
		feats += len(pool[i].own) + len(pool[i].opp)
		if len(pool[i].own) == 0 || len(pool[i].opp) == 0 {
			empty++
		}
	}

	t.Logf("positions      %d", len(pool))
	t.Logf("target         mean %+.3f  sd %.3f  (variance %.3f = constant-predictor MSE)", tMean, tSD, tSD*tSD)
	t.Logf("hand static    mean %+.3f  sd %.3f", sMean, sSD)
	t.Logf("correlation    %.3f", corr)
	t.Logf("hand MSE       %.3f", handMSE)
	t.Logf("features/pos   %.1f, %d samples with an empty side", float64(feats)/float64(len(pool)), empty)

	if empty > 0 {
		t.Errorf("%d samples have no features on one side: the network sees nothing for them", empty)
	}
	// The hand evaluation counts material. If it does not correlate with
	// the label at all, the labels are not what they are believed to be.
	if corr < 0.3 {
		t.Errorf("hand evaluation correlates %.3f with the label: the pool's targets "+
			"do not measure what the evaluation measures", corr)
	}
}
