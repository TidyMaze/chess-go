package engine

import (
	"chess/board"
	"os"
	"path/filepath"
	"testing"
)

func TestMutateWeightsKeepsKingAtZero(t *testing.T) {
	w := MutateWeights(DefaultWeights(), 0.5)
	if w[board.King] != 0 {
		t.Errorf("king weight should stay 0, got %v", w[board.King])
	}
}

func TestMutateWeightsStaysPositive(t *testing.T) {
	w := MutateWeights(DefaultWeights(), 0.5)
	for pt, v := range w {
		if pt == board.King {
			continue
		}
		if v <= 0 {
			t.Errorf("%v weight should stay positive, got %v", pt, v)
		}
	}
}

func TestResultScore(t *testing.T) {
	if got := ResultScore(board.White, true, board.White); got != 1.0 {
		t.Errorf("winner should score 1.0, got %v", got)
	}
	if got := ResultScore(board.Black, true, board.White); got != 0.0 {
		t.Errorf("loser should score 0.0, got %v", got)
	}
	if got := ResultScore(board.White, false, board.White); got != 0.5 {
		t.Errorf("draw should score 0.5, got %v", got)
	}
}

func TestEloFromWinRate(t *testing.T) {
	if got := EloFromWinRate(0.5); got != 0 {
		t.Errorf("even score should be elo 0, got %v", got)
	}
	if EloFromWinRate(0.9) <= 0 {
		t.Errorf("winning record should be positive elo")
	}
	if EloFromWinRate(0.1) >= 0 {
		t.Errorf("losing record should be negative elo")
	}
}

func TestSaveAndLoadChampionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "champion_weights.json")
	weights := MutateWeights(DefaultWeights(), 0.3)
	if err := SaveChampion(weights, 280, path); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	loaded, elo, err := LoadChampion(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if elo != 280 {
		t.Errorf("expected elo 280, got %v", elo)
	}
	for pt, v := range weights {
		if loaded[pt] != v {
			t.Errorf("%v: expected %v, got %v", pt, v, loaded[pt])
		}
	}
}

func TestLoadChampionMissingFile(t *testing.T) {
	_, _, err := LoadChampion(filepath.Join(t.TempDir(), "nope.json"))
	if !os.IsNotExist(err) {
		t.Errorf("expected not-exist error, got %v", err)
	}
}

func TestPlayGameReturnsAResult(t *testing.T) {
	w := DefaultWeights()
	result := PlayGame(w, w, 1, 20, nil)
	if result.Plies == 0 {
		t.Errorf("expected some moves to be played")
	}
	if result.Reason == "" {
		t.Errorf("expected a result reason")
	}
}
