package main

import (
	"math"
	"math/rand"
	"testing"
)

// Does the network actually learn?
//
// Nothing else in this package answers that. The training loop prints a
// held-out loss and compares it to the hand-written evaluation, and both
// numbers can look healthy while the network has learned nothing at all:
// on the Lichess pool it reported held-out 9.72 against the hand
// evaluation's 13.5, which reads as "more accurate" and was in fact worse
// than predicting a single constant for every position (9.80).
//
// So the test is against the constant predictor, not against the hand
// evaluation. A model that cannot beat the mean of its own targets has
// not learned, whatever else it beats.

// synthetic builds a dataset whose target is a known function of the
// active features, so a network that learns anything at all must beat the
// constant predictor by a wide margin. The signal is deliberately simple:
// the point is to catch a trainer that does not train, not to measure how
// hard a problem it can solve.
//
// Train and test must share one labelling function, or the test set is
// unlearnable by construction and every model looks like it is failing.
func synthetic(n int, values []float64, rng *rand.Rand) []sample {
	vocab := len(values)
	out := make([]sample, 0, n)
	for i := 0; i < n; i++ {
		var own, opp []int32
		target := 0.0
		for j := 0; j < 12; j++ {
			f := int32(rng.Intn(vocab))
			own = append(own, f)
			target += values[f]
		}
		for j := 0; j < 12; j++ {
			f := int32(rng.Intn(vocab))
			opp = append(opp, f)
			target -= values[f]
		}
		out = append(out, sample{
			own: own, opp: opp, target: target / 4, game: int32(i),
		})
	}
	return out
}

func featureValues(vocab int, rng *rand.Rand) []float64 {
	v := make([]float64, vocab)
	for i := range v {
		v[i] = rng.NormFloat64()
	}
	return v
}

func TestTrainingBeatsAConstantPredictor(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	values := featureValues(512, rng)
	train := synthetic(20000, values, rng)
	test := synthetic(4000, values, rng)

	order := make([]int32, len(train))
	for i := range order {
		order[i] = int32(i)
	}

	n := newNet(16, rng)
	baseline := constantBaseline(test)
	before := n.loss(test, 0.30, false)

	for e := 0; e < 40; e++ {
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		n.trainEpoch(train, order, 0.001, 0, 0.30, false, 4)
	}
	after := n.loss(test, 0.30, false)

	t.Logf("held-out loss %.4f -> %.4f, constant predictor %.4f", before, after, baseline)
	if math.IsNaN(after) || math.IsInf(after, 0) {
		t.Fatalf("training diverged: loss %v", after)
	}
	if after >= baseline*0.5 {
		t.Errorf("after 40 epochs the held-out loss is %.4f against a constant "+
			"predictor's %.4f: the network explains %.1f%% of the variance, "+
			"which is not learning", after, baseline, 100*(1-after/baseline))
	}
}

// The training loss must fall too. A held-out win with a flat training
// loss would mean the test set is easier, not that anything was fitted.
func TestTrainingLossFalls(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	train := synthetic(8000, featureValues(512, rng), rng)
	order := make([]int32, len(train))
	for i := range order {
		order[i] = int32(i)
	}
	n := newNet(16, rng)
	before := n.loss(train, 0.30, false)
	for e := 0; e < 40; e++ {
		n.trainEpoch(train, order, 0.001, 0, 0.30, false, 4)
	}
	after := n.loss(train, 0.30, false)
	t.Logf("training loss %.4f -> %.4f", before, after)
	if after >= before*0.5 {
		t.Errorf("training loss went %.4f -> %.4f: the optimiser is not fitting "+
			"even the data it is shown", before, after)
	}
}

// Weight decay is a flag the training runs have been sweeping. If it does
// nothing, every sweep over it measured noise.
func TestWeightDecayShrinksWeights(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	train := synthetic(4000, featureValues(512, rng), rng)
	order := make([]int32, len(train))
	for i := range order {
		order[i] = int32(i)
	}

	norm := func(n *net) float64 {
		s := 0.0
		for _, w := range n.w1 {
			s += float64(w) * float64(w)
		}
		return s
	}

	a := newNet(16, rand.New(rand.NewSource(13)))
	b := newNet(16, rand.New(rand.NewSource(13)))
	for e := 0; e < 10; e++ {
		// One worker: with several, Hogwild's races alone move the norms
		// enough for this comparison to pass or fail at random.
		a.trainEpoch(train, order, 0.001, 0, 0.30, false, 1)
		b.trainEpoch(train, order, 0.001, 0.05, 0.30, false, 1)
	}
	if norm(b) >= norm(a) {
		t.Errorf("decay 0.05 left the first-layer weight norm at %.4f against "+
			"%.4f with no decay: the decay parameter does nothing",
			norm(b), norm(a))
	}
}
