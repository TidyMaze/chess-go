package main

import (
	"strings"
	"testing"
)

// The gauntlet announced "the reference stays at depth N" whenever a clock
// was set, and printed it before the -ref-uci override had been applied.
// An external reference is given the same movetime, so that line was false
// exactly when it mattered, and reading it nearly cost a valid set of
// measurements: it says the opponent was crippled when it was not.
func TestClockNoteTellsTheTruthAboutTheReference(t *testing.T) {
	onClock := clockNote(1000, 4, true)
	if !strings.Contains(onClock, "1000 ms") || strings.Contains(onClock, "depth") {
		t.Errorf("an external reference shares the clock, so the note must say so: %q", onClock)
	}
	atDepth := clockNote(1000, 4, false)
	if !strings.Contains(atDepth, "depth 4") {
		t.Errorf("an in-process reference stays at its depth, and the note must say so: %q", atDepth)
	}
}
