package engine

import "testing"

func TestFiftyFadeRampsToZeroNearLimit(t *testing.T) {
	// At clock 80 (40 moves): floor is 0.5, score stays above 0.5 * score.
	score := 3.0
	at80 := fadeForFiftyMove(score, 80)
	if at80 < 1.5 {
		t.Errorf("fade at clock 80 should be >= 1.5, got %.3f", at80)
	}

	// At clock 98 (49 moves, 1 move before draw): score must fade near zero
	// so shuffling is not preferred over a pawn push.
	at98 := fadeForFiftyMove(score, 98)
	if at98 > 0.25 {
		t.Errorf("fade at clock 98 must collapse near 0, got %.3f", at98)
	}

	// At clock 100: game is drawn, returns 0.
	at100 := fadeForFiftyMove(score, 100)
	if at100 != 0 {
		t.Errorf("fade at clock 100 must be 0, got %.3f", at100)
	}
}
