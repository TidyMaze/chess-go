package engine

import (
	"os"
	"path/filepath"
	"testing"

	"chess/board"
	"chess/game"
)

func TestUCIEngineWrapper(t *testing.T) {
	if _, err := os.Stat(testStockfish); err != nil {
		t.Skip("no Stockfish at", testStockfish)
	}
	if _, err := NewStockfish("/nonexistent/stockfish", 0, 0); err == nil {
		t.Error("a missing binary started")
	}
	sf, err := NewStockfish(testStockfish, 20, 0) // no Elo cap: skill level only
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	start := game.New()
	legal, err := sf.LegalMoves(start)
	if err != nil || len(legal) != 20 {
		t.Errorf("perft 1 from the start: %d moves, %v", len(legal), err)
	}
	// Four promotions collapse to one queen promotion in this engine's rules.
	promo := mustFEN(t, "7k/P7/8/8/8/8/8/7K w - - 0 1")
	legal, err = sf.LegalMoves(promo)
	if err != nil || !legal["a7a8q"] || legal["a7a8n"] {
		t.Errorf("promotion normalisation: %v %v", legal, err)
	}
	for s, want := range map[string]bool{"e2e4": true, "e7e8q": true, "info": false, "Nodes": false, "e2e": false, "e2e44": false} {
		if looksLikeUCIMove(s) != want {
			t.Errorf("looksLikeUCIMove(%q) != %v", s, want)
		}
	}
	if _, mate, ok := sf.Evaluate(start, 1); !ok || mate {
		t.Errorf("start position evaluation: mate=%v ok=%v", mate, ok)
	}
	scholar := mustFEN(t, "r1bqkb1r/pppp1ppp/2n2n2/4p2Q/2B1P3/8/PPPP1PPP/RNB1K1NR w KQkq - 4 4")
	if _, mate, ok := sf.Evaluate(scholar, 2); !ok || !mate {
		t.Errorf("Qxf7# not reported as mate: mate=%v ok=%v", mate, ok)
	}
	sf.StaticEval(start) // either answer is fine, the parse must not hang
	if _, ok := sf.BestMove(start, 1, 20); !ok {
		t.Error("no move on a 20ms clock")
	}
	mated := mustFEN(t, "R5k1/5ppp/8/8/8/8/5PPP/6K1 b - - 0 1")
	if _, ok := sf.BestMove(mated, 1, 0); ok {
		t.Error("a move from a mated position")
	}
}

func TestTunedFileLoadsAppliesAndRejects(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "tuned.json")
	os.WriteFile(good, []byte(`{"material":[1,3,3,5,9,0],"mobility":[0,0.1,0.1,0.05,0.02,0],
	  "pst_scale":[1,1,1,1,1,1],"structure":{"Doubled":-0.2,"Isolated":-0.1,"PassedBase":0.2,
	  "PassedPerRank":0.1,"RookOpen":0.2,"RookSemiOpen":0.1,"KingShield":0.1}}`), 0o600)
	tf, err := LoadTunedFile(good)
	if err != nil {
		t.Fatal(err)
	}
	p := Strong(1)
	tf.Apply(&p)
	if p.Weights == nil || p.MobilityW == nil || p.PSTScale == nil || p.StructureW == nil || !p.Mobility {
		t.Errorf("apply left fields unset: %+v", p)
	}
	var nilFile *TunedFile
	nilFile.Apply(&p) // no-op
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{"material":[1,2],"mobility":[],"pst_scale":[]}`), 0o600)
	if _, err := LoadTunedFile(bad); err == nil {
		t.Error("short arrays accepted")
	}
	os.WriteFile(bad, []byte(`{not json`), 0o600)
	if _, err := LoadTunedFile(bad); err == nil {
		t.Error("malformed JSON accepted")
	}
	if _, err := LoadTunedFile(filepath.Join(dir, "absent.json")); err == nil {
		t.Error("missing file accepted")
	}
}

func TestSelfPlayTrainingLoop(t *testing.T) {
	w := Weights(&[6]float64{1, 3, 3, 5, 9, 0})
	if m := MutateWeights(w, 1.0); m == w {
		t.Error("mutation returned the same pointer")
	}
	if ResultScore(board.White, true, board.White) != 1 || ResultScore(board.White, true, board.Black) != 0 ||
		ResultScore(board.White, false, board.Black) != 0.5 {
		t.Error("ResultScore branches")
	}
	if EloFromWinRate(0.5) != 0 {
		t.Errorf("even score is %d Elo", EloFromWinRate(0.5))
	}
	plies := 0
	r := PlayGame(w, w, 1, 12, func(g *game.Game, n int, from, to board.Sq) { plies = n })
	if plies == 0 || r.Plies == 0 || r.Reason == "" {
		t.Errorf("game result %+v after %d plies", r, plies)
	}
	path := filepath.Join(t.TempDir(), "champ.json")
	if err := SaveChampion(w, 1234, path); err != nil {
		t.Fatal(err)
	}
	lw, elo, err := LoadChampion(path)
	if err != nil || elo != 1234 || lw == nil || lw[4] != 9 {
		t.Errorf("round trip: %v %d %v", lw, elo, err)
	}
	if _, _, err := LoadChampion(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("missing champion loaded")
	}
	if score, _ := EvaluateVsFixedBot(w, 1, 1, 2, 12); score < 0 || score > 1 {
		t.Errorf("score %v", score)
	}
	gens := 0
	best, records := Train(Config{Generations: 1, GamesPerGen: 2, PopulationSize: 2, Depth: 1, MaxMoves: 12,
		BenchmarkGames: 2, RatchetGames: 2, Patience: 1, ChampionPath: filepath.Join(t.TempDir(), "c.json"),
		OnGeneration: func(GenerationRecord) { gens++ }})
	if best == nil || len(records) == 0 || gens == 0 {
		t.Errorf("train: best %v records %d gens %d", best, len(records), gens)
	}
	if maxInt(2, 3) != 3 || maxInt(4, 1) != 4 {
		t.Error("maxInt")
	}
}
