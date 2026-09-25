package main

import (
	"strings"
	"testing"
	"time"
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

// A benchmark has a wall-clock budget, not a game count: -max-seconds 0 means
// play every game, anything else stops starting games after that long.
func TestMaxSecondsBecomesAMatchDeadline(t *testing.T) {
	now := time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)
	if d := deadlineFor(0, now); !d.IsZero() {
		t.Errorf("-max-seconds 0 gave deadline %v, want none", d)
	}
	if d := deadlineFor(540, now); !d.Equal(now.Add(540 * time.Second)) {
		t.Errorf("-max-seconds 540 gave %v, want %v", d, now.Add(540*time.Second))
	}
}

// The champion's own network on the plain challenger beat the champion at
// depth 4, and four ladder rungs were adopted on that gap.
func TestANetworkIsOnlyRacedAgainstAChampionFromAChampionFile(t *testing.T) {
	if networkAgainstChampion("net.json", "", "", "champion.json") == nil {
		t.Error("-halfkp against -ref-champion must be refused")
	}
	if networkAgainstChampion("", "net.json", "", "champion.json") == nil {
		t.Error("-net against -ref-champion must be refused")
	}
	for _, ok := range [][4]string{
		{"", "", "cand.json", "champion.json"},
		{"net.json", "", "", ""},
		{"", "", "", "champion.json"},
	} {
		if err := networkAgainstChampion(ok[0], ok[1], ok[2], ok[3]); err != nil {
			t.Errorf("%v must be allowed: %v", ok, err)
		}
	}
}
