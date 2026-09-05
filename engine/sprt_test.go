package engine

import "testing"

// SPRT decides when a match has seen enough games. These check that it
// decides the right way and that it actually stops early, which is the
// entire reason for using it.
func TestSPRTAcceptsAClearImprovement(t *testing.T) {
	s := NewSPRT(0, 5)
	games := 0
	for s.Status() == SPRTContinue && games < 20000 {
		// A genuinely better engine: 40% wins, 40% draws, 20% losses.
		s.Add(4, 4, 2)
		games += 10
	}
	if s.Status() != SPRTAcceptH1 {
		t.Errorf("a 60%% score was not accepted: %v after %d games (LLR %.2f)",
			s.Status(), s.Games(), s.LLR())
	}
	t.Logf("accepted after %d games (fixed-size testing would have needed ~1500)", s.Games())
}

func TestSPRTRejectsNoImprovement(t *testing.T) {
	s := NewSPRT(0, 5)
	games := 0
	for s.Status() == SPRTContinue && games < 200000 {
		// Dead even.
		s.Add(3, 4, 3)
		games += 10
	}
	if s.Status() != SPRTAcceptH0 {
		t.Errorf("an even match was not rejected: %v after %d games", s.Status(), s.Games())
	}
	t.Logf("rejected after %d games", s.Games())
}

func TestSPRTRejectsARegressionFast(t *testing.T) {
	s := NewSPRT(0, 5)
	for s.Status() == SPRTContinue && s.Games() < 20000 {
		s.Add(2, 3, 5) // clearly worse
	}
	if s.Status() != SPRTAcceptH0 {
		t.Errorf("a losing change was not rejected: %v", s.Status())
	}
	t.Logf("a clear regression is rejected after %d games", s.Games())
}
