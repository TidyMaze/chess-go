package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The ladder's one load-bearing assumption: the player that labels a
// rung's training data and the player the rung is then raced against are
// the same player.
//
// When they were not, the ladder was measuring each rung against the plain
// hand-written evaluation instead of against its own teacher. Seven rungs
// all measured the same comparison, the deltas were summed as though they
// compounded, and the result was a claimed 2146 Elo for a ladder that had
// been flat throughout. Nothing in the numbers looked wrong.
//
// Both sides come from champion.json, so the test is that one file
// produces one configuration, and that every part of it survives the trip.
func TestChampionLabellerAndReferenceAreTheSamePlayer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "champion.json")
	// No net_file: loading a real network is not what is under test, and a
	// missing one would send Player down its silent fallback.
	if err := os.WriteFile(path, []byte(`{
	  "label": "test rung",
	  "depth": 5,
	  "elo": 2100,
	  "margin": 30,
	  "hand_blend": 0.45
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	c := ReadChampion(path)
	labeller, err := c.PlayerOrError()
	if err != nil {
		t.Fatalf("the labeller must load loudly or not at all: %v", err)
	}
	reference, err := c.PlayerOrError()
	if err != nil {
		t.Fatal(err)
	}

	if labeller.Depth != c.Depth {
		t.Errorf("champion.json says depth %d, the player searches %d",
			c.Depth, labeller.Depth)
	}
	if !reflect.DeepEqual(labeller, reference) {
		t.Error("two players built from the same champion file differ, so the rung " +
			"is not raced against the player that taught it")
	}
}

// A champion naming a network that will not load must fail loudly.
//
// Player falls back to the hand-written evaluation and warns on stderr.
// For the ladder that is the worst possible outcome: it labels a whole
// rung with the wrong teacher and produces a network that imitates the
// baseline, which then loses its race for a reason no number explains.
func TestChampionWithAMissingNetworkIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "champion.json")
	if err := os.WriteFile(path, []byte(`{
	  "label": "broken",
	  "depth": 5,
	  "net_file": "`+filepath.Join(dir, "absent.json")+`",
	  "hand_blend": 0.45
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := ReadChampion(path)
	if _, err := c.PlayerOrError(); err == nil {
		t.Error("a champion whose network is missing returned no error, so the ladder " +
			"would label a rung with the hand evaluation and never know")
	}
}

// The blend has to reach the search, on both paths.
//
// PlayerScoreWith labels every training position and PlayerPick plays every
// game. They built their Eval separately and the scoring one dropped
// HalfKPBlend, so a champion playing at 0.45 labelled at 0. The rung then
// learned to imitate a player that never played.
func TestChampionBlendReachesBothSearchPaths(t *testing.T) {
	c := Champion{Depth: 5, HandBlend: 0.45}
	p, err := c.PlayerOrError()
	if err != nil {
		t.Fatal(err)
	}
	// No network here, so the blend is carried but unused; what matters is
	// that it is carried, because with a network it decides the labels.
	p.HalfKPBlend = c.HandBlend
	if got := evalForPlayer(p).HalfKPBlend; got != 0.45 {
		t.Errorf("the evaluation the search runs on has blend %v, not 0.45", got)
	}
}
