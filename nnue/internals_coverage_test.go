package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"chess/engine"
)

func TestWriteJSONSkipsWhatItCannotMarshal(t *testing.T) {
	dir := inScratch(t)
	writeJSON("ok.json", map[string]any{"a": 1})
	if _, err := os.Stat(filepath.Join(dir, "ok.json")); err != nil {
		t.Errorf("nothing written: %v", err)
	}
	writeJSON("bad.json", map[string]any{"nan": math.NaN()})
	if _, err := os.Stat(filepath.Join(dir, "bad.json")); err == nil {
		t.Error("a file was written for something that does not marshal")
	}
}

func TestBlendedTargetClamps(t *testing.T) {
	if v := blendedTarget(100, 1, 1.0); v != 12 {
		t.Errorf("a huge score clamps to 12, got %v", v)
	}
	if v := blendedTarget(-100, 0, 1.0); v != -12 {
		t.Errorf("a huge loss clamps to -12, got %v", v)
	}
	if v := blendedTarget(2, 1, 0.5); math.Abs(v-3) > 1e-9 {
		t.Errorf("half score half outcome: %v, want 3", v)
	}
	if resultPawns(1) != 4 || resultPawns(0) != -4 || resultPawns(0.5) != 0 {
		t.Error("result in pawns")
	}
}

func TestNetSmoothingAndSigmoidLoss(t *testing.T) {
	n := newNetForTest(4)
	for i := range n.w1 {
		n.w1[i] = float32(i % 7)
	}
	before := append([]float32(nil), n.w1...)
	n.smooth(0) // a no-op
	for i := range n.w1 {
		if n.w1[i] != before[i] {
			t.Fatal("alpha 0 changed the weights")
		}
	}
	n.smooth(0.5)
	same := true
	for i := range n.w1 {
		if n.w1[i] != before[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("smoothing changed nothing")
	}
	// The loss on a sigmoid target goes through the probability path.
	data := []sample{{own: []int32{1, 2}, opp: []int32{3}, target: 0.6}}
	a := n.loss(data, 0.30, true)
	b := n.loss(data, 0.30, false)
	if math.IsNaN(a) || math.IsNaN(b) || a == b {
		t.Errorf("sigmoid loss %v, plain loss %v", a, b)
	}
}

func TestGenerationWithProgressAndStockfishTeacher(t *testing.T) {
	inScratch(t)
	champion := engine.Strong(1)
	stats := &genStats{}
	old := trainProgress
	fired := 0
	trainProgress = func(done, total int, rate float64, eta time.Duration) { fired++ }
	defer func() { trainProgress = old }()
	// Games short enough to end in every way the loop handles.
	samples := generate(champion, 4, 1, 1, 30, 0.8, 0.30, 0.35, false, 0, nil, 1, 1, stats, nil, nil)
	if len(samples) == 0 {
		t.Error("no samples generated")
	}
	if _, err := os.Stat("/opt/homebrew/bin/stockfish"); err == nil {
		sf, err := engine.NewStockfish("/opt/homebrew/bin/stockfish", 0, 1320)
		if err != nil {
			t.Fatal(err)
		}
		defer sf.Close()
		teachers := make(chan *engine.UCIEngine, 1)
		teachers <- sf
		if s := generate(champion, 2, 1, 1, 20, 0.8, 0.30, 0.35, false, 0, teachers, 1, 1, stats, nil, nil); len(s) == 0 {
			t.Error("no samples from the Stockfish teacher")
		}
	}
	// Sigmoid targets, and openings supplied.
	openings := []string{"rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1"}
	if s := generate(champion, 2, 1, 1, 20, 0.8, 0.30, 0.35, true, 0, nil, 1, 1, stats, openings, nil); len(s) == 0 {
		t.Error("no samples with sigmoid targets")
	}
	if stats.games == 0 {
		t.Error("no games counted")
	}
}
