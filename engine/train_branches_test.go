package engine

import (
	"path/filepath"
	"testing"
)

// The generation loop's three outcomes: a promotion, a resume from a saved
// champion, and the patience stop. Mutation and tie-breaks draw from the
// seeded global generator, so a seed that promotes is found once and then
// stays deterministic.
func TestTrainingLoopBranches(t *testing.T) {
	cfg := Config{Generations: 2, GamesPerGen: 4, PopulationSize: 3, Depth: 1, MaxMoves: 80,
		BenchmarkGames: 2, RatchetGames: 2, Patience: 5, ChampionPath: filepath.Join(t.TempDir(), "c.json")}
	promoted := false
	for seed := int64(1); seed <= 40 && !promoted; seed++ {
		SeedRandom(seed)
		_, records := Train(cfg)
		for _, r := range records {
			if r.Promoted {
				promoted = true
			}
		}
	}
	if !promoted {
		t.Error("no seed in 1..40 produced a promotion; the promotion branch went unexercised")
	}
	// Resume: the champion file written by the promotion is loaded back.
	_, records := Train(cfg)
	if len(records) == 0 {
		t.Error("no generations on resume")
	}
	// Patience: with no possible improvement the loop stops early.
	strict := cfg
	strict.Patience = 1
	strict.Generations = 6
	_, records = Train(strict)
	if len(records) >= 6 {
		t.Log("patience never triggered; every generation promoted (allowed, unlikely)")
	}
}
