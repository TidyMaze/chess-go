package engine

import (
	"time"

	"chess/game"
)

// Stability-based time management, behind the "stabletime" feature flag.
//
// The plain timed search spends its whole budget on every move. A move whose
// best reply has not changed for several plies does not need it, and one
// whose best move keeps changing, or whose score just fell, needs more. So
// the budget becomes a soft target: after each completed iteration the main
// thread decides, from the elapsed time and the history of best moves and
// scores, whether to start another one. The hard limit, twice the target
// unless the caller lowers it, is enforced by the node-level abort.
//
// The constants below are the defaults of the SearchTune Stable* fields: a
// tune overrides the hard multiple, the early stop and the factor's bounds.
const (
	// stableHardMultiple is the hard limit as a multiple of the soft target.
	stableHardMultiple = 2
	// stableEarlyFraction and stableEarlyIters: past this share of the
	// target, stop once the best move has been the same for this many
	// iterations with no score drop.
	stableEarlyFraction = 0.5
	stableEarlyIters    = 4
	// stableDropTolerance is how far a score may dip between two iterations
	// and still count as no drop: odd and even depths disagree by a few
	// hundredths of a pawn on a perfectly quiet position.
	stableDropTolerance = 0.1
	// stableFallMargin is the score fall, in pawns, that counts as the
	// position getting worse and earns more time.
	stableFallMargin = 0.3
	// The factor on the soft target starts at 1, grows by stableGrow when the
	// best move changes or the score falls, shrinks by stableShrink while it
	// holds, and stays within [stableMinFactor, stableMaxFactor].
	stableGrow      = 1.5
	stableShrink    = 0.85
	stableMinFactor = 0.6
	stableMaxFactor = 2.0
	// The next iteration is predicted at the last one times the ratio of
	// the last two, clamped: the branching factor varies from 1.9 to 4.6
	// per ply depending on the position.
	stableMinGrowth     = 1.5
	stableMaxGrowth     = 4.0
	stableDefaultGrowth = 2.0
)

// iterRecord is one completed iteration of the main thread: its best move,
// its score from the mover's point of view, and how long it took.
type iterRecord struct {
	move  game.Move
	score float64
	took  time.Duration
}

// stableNow is the clock the stop decisions read. Only a test replaces it;
// the node-level abort reads the real clock regardless.
var stableNow = time.Now

// orDefault is the tune itself, or the defaults when there is none: the
// constants above, which DefaultSearchTune holds.
func (t *SearchTune) orDefault() *SearchTune {
	if t != nil {
		return t
	}
	d := DefaultSearchTune()
	return &d
}

// stableHardLimit is the absolute limit for a soft target: StableHard times
// it (twice by default), or less when the caller asks for less.
func stableHardLimit(tune *SearchTune, soft, callerHard time.Duration) time.Duration {
	hard := time.Duration(tune.orDefault().StableHard * float64(soft))
	if callerHard > 0 && callerHard < hard {
		return callerHard
	}
	return hard
}

// stableFactor folds the iteration history into the multiplier on the soft
// target.
func stableFactor(tune *SearchTune, hist []iterRecord) float64 {
	tn := tune.orDefault()
	f := 1.0
	for i := 1; i < len(hist); i++ {
		if hist[i].move != hist[i-1].move || hist[i].score < hist[i-1].score-tn.StableDrop {
			f = min(f*tn.StableGrow, tn.StableMax)
		} else {
			f = max(f*stableShrink, tn.StableMin)
		}
	}
	return f
}

// settled reports whether the last StableIters iterations agree on the best
// move without the score dropping. A StableIters below 1 reads as 1.
func settled(tune *SearchTune, hist []iterRecord) bool {
	n := max(int(tune.orDefault().StableIters), 1)
	if len(hist) < n {
		return false
	}
	tail := hist[len(hist)-n:]
	for i := 1; i < len(tail); i++ {
		if tail[i].move != tail[0].move || tail[i].score < tail[i-1].score-stableDropTolerance {
			return false
		}
	}
	return true
}

// predictedNext is how long the next iteration should take.
func predictedNext(hist []iterRecord) time.Duration {
	if len(hist) == 0 {
		return 0
	}
	last := hist[len(hist)-1].took
	growth := stableDefaultGrowth
	if len(hist) >= 2 && hist[len(hist)-2].took > 0 {
		growth = float64(last) / float64(hist[len(hist)-2].took)
		growth = min(max(growth, stableMinGrowth), stableMaxGrowth)
	}
	return time.Duration(float64(last) * growth)
}

// stableTimeStop decides, after a completed iteration, whether the search
// stops rather than starting the next one.
func stableTimeStop(tune *SearchTune, elapsed, soft, hard time.Duration, hist []iterRecord) bool {
	switch {
	case elapsed >= hard:
		return true
	case float64(elapsed) > tune.orDefault().StableEarly*float64(soft) && settled(tune, hist):
		return true
	case float64(elapsed) > stableFactor(tune, hist)*float64(soft):
		return true
	}
	// An iteration that cannot finish before the hard limit would be cut
	// off, so it is not started.
	return elapsed+predictedNext(hist) > hard
}
