package engine

import (
	"testing"
	"time"
)

// A match stopped by its deadline keeps only the openings it played with both
// colours: one side of a pair alone carries whatever edge the start position
// gives, which the pairing exists to cancel.
func TestTheTallyKeepsOnlyCompletePairs(t *testing.T) {
	got := tallyPairs([]float64{1, 0.5, 1, gameSkipped, gameSkipped, 0})
	want := MatchResult{Wins: 1, Draws: 1}
	if got != want {
		t.Errorf("tally %+v, want %+v", got, want)
	}
}

func TestAMatchPastItsDeadlineStartsNoGame(t *testing.T) {
	defer func() { MatchDeadline = time.Time{} }()
	quick := Player{Depth: 1}
	MatchDeadline = time.Now().Add(-time.Second)
	if res := PlayMatch(quick, quick, 4, 20); res.Wins+res.Draws+res.Losses != 0 {
		t.Errorf("a passed deadline still played %+v", res)
	}
	MatchDeadline = time.Time{}
	if res := PlayMatch(quick, quick, 4, 20); res.Wins+res.Draws+res.Losses != 4 {
		t.Errorf("no deadline must play every game, got %+v", res)
	}
}
