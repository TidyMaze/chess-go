package engine

import "math"

// Sequential Probability Ratio Testing, the method Fishtest uses to
// decide whether a change to Stockfish is an improvement.
//
// Every match in this project so far has been a fixed size chosen in
// advance, which is wasteful in both directions. A change that is
// obviously bad still gets its full 1500 games, and a change that is
// genuinely good still gets no more than the budget allowed even when a
// few hundred more games would settle it.
//
// SPRT instead tests two hypotheses after every game:
//
//	H0: the change is worth elo0 (typically 0, "no gain")
//	H1: the change is worth elo1 (typically 5, "a gain worth having")
//
// and stops as soon as the accumulated evidence favours one strongly
// enough. The log-likelihood ratio walks between two bounds set by the
// error rates alpha and beta; crossing the upper bound accepts H1,
// crossing the lower accepts H0, and in between the test keeps playing.
//
// The practical effect is that clearly-bad changes are rejected in a few
// hundred games instead of 1500, which matters here because measurement
// cost, not implementation time, has been the limiting factor throughout.

// SPRT holds the state of a running sequential test.
type SPRT struct {
	Elo0, Elo1          float64 // the two hypotheses, in Elo
	Alpha, Beta         float64 // false-positive and false-negative rates
	wins, draws, losses int
}

// NewSPRT starts a test of "worth at least elo1" against "worth elo0",
// at the 5% error rates Fishtest uses by default.
func NewSPRT(elo0, elo1 float64) *SPRT {
	return &SPRT{Elo0: elo0, Elo1: elo1, Alpha: 0.05, Beta: 0.05}
}

func (s *SPRT) Add(wins, draws, losses int) {
	s.wins += wins
	s.draws += draws
	s.losses += losses
}

func (s *SPRT) Games() int { return s.wins + s.draws + s.losses }

// eloToScore converts an Elo difference to an expected score.
func eloToScore(elo float64) float64 { return 1 / (1 + math.Pow(10, -elo/400)) }

// LLR is the log-likelihood ratio of H1 against H0.
//
// This is the normalised (five-outcome collapsed to three) form used by
// Fishtest: the score is treated as a random variable whose mean and
// variance are estimated from the result so far, and the ratio follows
// from the difference in means under the two hypotheses.
func (s *SPRT) LLR() float64 {
	n := float64(s.Games())
	if n < 2 {
		return 0
	}
	w := float64(s.wins) / n
	d := float64(s.draws) / n
	l := float64(s.losses) / n

	score := w + d/2
	// Variance of the per-game score. Guarded because a match that is all
	// draws, or all wins, has zero variance and would divide by nothing.
	variance := w*(1-score)*(1-score) + d*(0.5-score)*(0.5-score) + l*score*score
	if variance <= 1e-9 {
		variance = 1e-9
	}

	p0, p1 := eloToScore(s.Elo0), eloToScore(s.Elo1)
	return n * (p1 - p0) * (2*score - p0 - p1) / (2 * variance)
}

// Bounds returns the lower and upper decision thresholds.
func (s *SPRT) Bounds() (lower, upper float64) {
	return math.Log(s.Beta / (1 - s.Alpha)), math.Log((1 - s.Beta) / s.Alpha)
}

// SPRTResult is the verdict of a sequential test.
type SPRTResult int

const (
	SPRTContinue SPRTResult = iota
	SPRTAcceptH1            // the change is an improvement
	SPRTAcceptH0            // the change is not an improvement
)

func (r SPRTResult) String() string {
	switch r {
	case SPRTAcceptH1:
		return "accepted: the change is an improvement"
	case SPRTAcceptH0:
		return "rejected: the change is not an improvement"
	}
	return "inconclusive"
}

// Status reports whether enough evidence has accumulated to stop.
func (s *SPRT) Status() SPRTResult {
	lower, upper := s.Bounds()
	llr := s.LLR()
	switch {
	case llr >= upper:
		return SPRTAcceptH1
	case llr <= lower:
		return SPRTAcceptH0
	}
	return SPRTContinue
}

// Elo estimates the current Elo difference, for reporting only. The
// decision is made by the likelihood ratio, not by this.
func (s *SPRT) Elo() float64 {
	n := float64(s.Games())
	if n == 0 {
		return 0
	}
	score := (float64(s.wins) + float64(s.draws)/2) / n
	if score <= 0 {
		return -800
	}
	if score >= 1 {
		return 800
	}
	return -400 * math.Log10(1/score-1)
}
