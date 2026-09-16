package main

import (
	"math"
	"testing"
)

// The self-play generator labels its own games, so it is the data source
// that can take the engine past its teacher: a game result is not the
// search's opinion. It blended in pawn space like the import did, which
// caps a win at +4 and so tells the network that a won position it scored
// at +8 was really worth less. selfPlayTarget must use probability space
// when a steepness is given and leave the old behaviour alone at zero.
func TestSelfPlayTargetUsesProbabilitySpaceWhenAsked(t *testing.T) {
	const lambda, k = 0.8, 0.30
	const winning, won = 8.0, 1.0

	pawn := selfPlayTarget(winning, won, lambda, 0, false, k)
	if pawn >= winning {
		t.Errorf("with no steepness the old pawn blend should apply and lower the target, got %+.2f", pawn)
	}
	prob := selfPlayTarget(winning, won, lambda, k, false, k)
	if prob < winning {
		t.Errorf("probability space must never lower a won position: %+.2f against %+.1f", prob, winning)
	}

	// The win-probability target, when the network is trained to output
	// one, is untouched by this and stays a probability.
	p := selfPlayTarget(winning, won, lambda, k, true, k)
	if p < 0 || p > 1 {
		t.Errorf("a sigmoid-target network wants a probability, got %v", p)
	}
	if math.Abs(p-(lambda*sigmoid(winning, k)+(1-lambda)*won)) > 1e-9 {
		t.Errorf("the sigmoid target changed: %v", p)
	}
}
