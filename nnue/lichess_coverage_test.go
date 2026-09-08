package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic lines in the shape of the Lichess evaluation dump. The real
// dump is disqualified as a training source; the code that reads it is
// still code, and is exercised here on made-up data only.
const syntheticDump = `{"fen":"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq -","evals":[{"pvs":[{"cp":20,"line":"e2e4 e7e5 g1f3"}],"depth":30},{"pvs":[{"cp":25,"line":"d2d4 d7d5"}],"depth":40}]}
{"fen":"r1bqkb1r/pppp1ppp/2n2n2/4p2Q/2B1P3/8/PPPP1PPP/RNB1K1NR w KQkq -","evals":[{"pvs":[{"mate":1,"line":"h5f7"}],"depth":20}]}
{"fen":"8/8/4k3/8/8/8/8/K6R w - -","evals":[{"pvs":[{"cp":900,"line":"h1h6"}],"depth":30}]}
{"fen":"6k1/5ppp/8/8/8/8/5PPP/6K1 w - -","evals":[]}
not json at all
{"fen":"rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3","evals":[{"pvs":[{"cp":-30,"line":"c7c5 g1f3"}],"depth":30}]}
`

func TestEvalLineParsing(t *testing.T) {
	if padFEN("a b c d") != "a b c d 0 1" || padFEN("a b c d 0 1") != "a b c d 0 1" {
		t.Error("padFEN")
	}
	var e evalLine
	if _, ok := e.bestEval(); ok {
		t.Error("no evals gave a score")
	}
	if _, ok := e.bestPV(); ok {
		t.Error("no evals gave a PV")
	}
	if _, ok := e.bestLine(); ok {
		t.Error("no evals gave a line")
	}
}

func TestExtractFromASyntheticDump(t *testing.T) {
	dir := t.TempDir()
	openings := filepath.Join(dir, "openings.txt")
	if err := ExtractOpenings(strings.NewReader(syntheticDump), openings, 10, 4); err != nil {
		t.Fatal(err)
	}
	list, err := LoadOpenings(openings)
	if err != nil || len(list) == 0 {
		t.Fatalf("openings %v %v", list, err)
	}
	if _, err := LoadOpenings(filepath.Join(dir, "absent")); err == nil {
		t.Error("missing openings loaded")
	}
	book := filepath.Join(dir, "book.txt")
	if err := ExtractBook(strings.NewReader(syntheticDump), book, 10, 4); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(book); !strings.Contains(string(data), "|") {
		t.Errorf("book has no FEN|move line: %q", data)
	}
	tuning := filepath.Join(dir, "tuning.jsonl")
	if err := ExtractTuning(strings.NewReader(syntheticDump), tuning, 10, true, 0.35); err != nil {
		t.Fatal(err)
	}
	if err := ExtractTuning(strings.NewReader(syntheticDump), tuning, 1, false, 0.35); err != nil {
		t.Fatal(err)
	}
	// Unwritable destinations are errors, not silence.
	bad := filepath.Join(dir, "no", "such", "dir", "x")
	if ExtractOpenings(strings.NewReader(syntheticDump), bad, 1, 4) == nil ||
		ExtractBook(strings.NewReader(syntheticDump), bad, 1, 4) == nil ||
		ExtractTuning(strings.NewReader(syntheticDump), bad, 1, false, 0.35) == nil {
		t.Error("writing into a missing directory succeeded")
	}
}

func TestImportLichessOnSyntheticData(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool.bin")
	if err := ImportLichess(strings.NewReader(syntheticDump), pool, 100, 0.35, true, false); err != nil {
		t.Fatal(err)
	}
	samples, err := loadPool(pool, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) == 0 {
		t.Error("no positions imported from the synthetic dump")
	}
	// Resume from the marker: nothing new to add.
	if err := ImportLichess(strings.NewReader(syntheticDump), pool, 100, 0.35, false, true); err != nil {
		t.Fatal(err)
	}
	if ImportLichess(strings.NewReader(syntheticDump), filepath.Join(t.TempDir(), "no", "dir", "p.bin"), 1, 0.35, false, false) == nil {
		t.Error("an unwritable pool path succeeded")
	}
}

func TestPoolAndCheckpointPersistence(t *testing.T) {
	dir := t.TempDir()
	pool := filepath.Join(dir, "pool.bin")
	s := []sample{{own: []int32{1, 2}, opp: []int32{3}, target: 0.5, static: 0.25, game: 1},
		{own: []int32{4}, opp: []int32{5, 6}, target: -0.5, static: 0, game: 2}}
	if err := appendPool(pool, s); err != nil {
		t.Fatal(err)
	}
	got, err := loadPool(pool, 1) // keep the most recent one
	if err != nil || len(got) != 1 || got[0].game != 2 {
		t.Errorf("trimmed load: %d samples %v", len(got), err)
	}
	if _, err := loadPool(filepath.Join(dir, "absent.bin"), 0); err == nil {
		t.Error("missing pool loaded")
	}
	if appendPool(filepath.Join(dir, "no", "dir", "p.bin"), s) == nil {
		t.Error("unwritable pool appended")
	}
	n := newNetForTest(4)
	ck := filepath.Join(dir, "net.gob")
	if err := saveNet(ck, n, 3, 120); err != nil {
		t.Fatal(err)
	}
	m, gen, elo, err := loadNet(ck, 4)
	if err != nil || m.h != 4 || gen != 3 || elo != 120 {
		t.Errorf("round trip: h=%v gen=%d elo=%d err=%v", m, gen, elo, err)
	}
	if _, _, _, err := loadNet(ck, 8); err == nil {
		t.Error("a checkpoint of the wrong width loaded")
	}
	if _, _, _, err := loadNet(filepath.Join(dir, "absent.gob"), 4); err == nil {
		t.Error("a missing checkpoint loaded")
	}
	os.WriteFile(ck, []byte("garbage"), 0o600)
	if _, _, _, err := loadNet(ck, 4); err == nil {
		t.Error("garbage decoded")
	}
	if saveNet(filepath.Join(dir, "no", "dir", "n.gob"), n, 0, 0) == nil {
		t.Error("unwritable checkpoint saved")
	}
}

func TestEmitEvalCheck(t *testing.T) {
	if _, err := os.Stat("../champion_net.json"); err != nil {
		t.Skip("no champion network")
	}
	out := filepath.Join(t.TempDir(), "ec.json")
	if err := emitEvalCheck(out, "../champion_net.json"); err != nil {
		t.Fatal(err)
	}
	if emitEvalCheck(out, filepath.Join(t.TempDir(), "absent.json")) == nil {
		t.Error("a missing network emitted a check")
	}
	if emitEvalCheck(filepath.Join(t.TempDir(), "no", "dir", "x.json"), "../champion_net.json") == nil {
		t.Error("an unwritable output succeeded")
	}
}
