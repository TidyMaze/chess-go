package engine

import (
	"testing"
	"time"
)

// skipIfMachineBusy skips a timing test when this process cannot hold a
// processor long enough for a millisecond budget to mean anything.
//
// A 10 ms budget test overshot to 587% three times in one evening, every
// time because a data generator held all ten cores, and each false red
// cost a five minute suite run to diagnose. The test was right that the
// budget was missed and wrong about who missed it.
//
// What breaks the budget is preemption, not throughput: a fixed spin
// still finished in 11 ms under a load average of 91, while the gap
// between two consecutive clock reads reached 30 ms. A search descheduled
// for 30 ms cannot stop at 10 ms however carefully it checks the time.
// So the probe measures that gap directly.
func skipIfMachineBusy(t *testing.T) {
	t.Helper()
	const window = 20 * time.Millisecond
	// Three milliseconds is far above an idle machine, which reads the
	// clock again within microseconds, and far below the 20 to 30 ms
	// observed while the generator was running.
	const tolerated = 3 * time.Millisecond

	var worst time.Duration
	start := time.Now()
	last := start
	for time.Since(start) < window {
		now := time.Now()
		if gap := now.Sub(last); gap > worst {
			worst = gap
		}
		last = now
	}
	if worst > tolerated {
		t.Skipf("machine is busy: this goroutine lost the processor for %v inside a %v window, so a millisecond budget cannot be measured here",
			worst, window)
	}
}
