package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chess/engine"
	"time"
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

func TestPGNTokenAndReaderEdges(t *testing.T) {
	for _, tok := range []string{"", "*", "$12", "1-0", "1/2-1/2", "0-1"} {
		if _, ok := moveToken(tok); ok {
			t.Errorf("%q accepted as a move", tok)
		}
	}
	if m, ok := moveToken("12...Nf6"); !ok || m != "Nf6" {
		t.Errorf("move number stripping: %q %v", m, ok)
	}
	if _, ok := moveToken("7."); ok {
		t.Error("a bare move number is not a move")
	}
	// A game with no result tag is skipped by the reader.
	out := make(chan pgnGame, 4)
	go readPGN(strings.NewReader("[White \"a\"]\n\n1. e4 e5\n"), out)
	n := 0
	for range out {
		n++
	}
	if n != 0 {
		t.Errorf("a game without a result was emitted (%d)", n)
	}
}

func TestImportPGNEdges(t *testing.T) {
	dir := t.TempDir()
	pool := filepath.Join(dir, "p.bin")
	labeller := engine.Strong(1)
	// Positions are clamped to +/-12 pawns: label from a hopeless position.
	lopsided := "[Result \"1-0\"]\n\n1. e4 e5 2. Qh5 Nc6 3. Bc4 Nf6 4. Qxf7# 1-0\n"
	if err := ImportPGN(strings.NewReader(lopsided), pool, 0, 1, 0, 0.8, 5.0, 0, false, labeller); err != nil {
		t.Fatal(err)
	}
	// Progress reporting and the keep cap.
	if err := ImportPGN(strings.NewReader(operaGamePGN), pool, 3, 1, 0, 0.8, 0.35, time.Nanosecond, false, labeller); err != nil {
		t.Fatal(err)
	}
	// Resume from the marker.
	if err := ImportPGN(strings.NewReader(operaGamePGN), pool, 0, 1, 0, 0.8, 0.35, 0, true, labeller); err != nil {
		t.Fatal(err)
	}
	// A game whose moves do not replay is counted, not fatal.
	if err := ImportPGN(strings.NewReader("[Result \"1-0\"]\n\n1. e4 Qh8 1-0\n"), pool, 0, 1, 0, 0.8, 0.35, 0, false, labeller); err != nil {
		t.Fatal(err)
	}
	if err := ImportPGN(strings.NewReader(operaGamePGN), filepath.Join(dir, "no", "dir", "p.bin"), 0, 1, 0, 0.8, 0.35, 0, false, labeller); err == nil {
		t.Error("an unwritable pool succeeded")
	}
}

func TestSplitAndBaselineEdges(t *testing.T) {
	if constantBaseline(nil) != 0 {
		t.Error("empty baseline")
	}
	one := []sample{{game: 1, target: 1}}
	// A held-out share larger than the pool keeps every game.
	tr, ho := splitByGame(one, rand.New(rand.NewSource(1)), 100)
	if len(tr)+len(ho) != 1 {
		t.Errorf("split lost samples: %d + %d", len(tr), len(ho))
	}
}

func TestOpeningFromAnExhaustedGame(t *testing.T) {
	// 600 plies from the start reaches a finished game long before the end.
	if g := randomOpeningGame(rand.New(rand.NewSource(1)), 600); g == nil {
		t.Error("no game")
	}
}

// The deepest-analysis pickers, on the shapes a dump actually contains:
// several depths out of order, a record whose deepest entry has an empty
// line, a first token too short to be a move, and no usable entry at all.
func TestBestPVAndBestLinePickTheDeepestUsableEntry(t *testing.T) {
	line := func(specs ...[2]string) evalLine {
		var e evalLine
		for _, s := range specs {
			var ev struct {
				PVs []struct {
					CP   *int   `json:"cp"`
					Mate *int   `json:"mate"`
					Line string `json:"line"`
				} `json:"pvs"`
				Depth int `json:"depth"`
			}
			d := 0
			fmt.Sscanf(s[0], "%d", &d)
			ev.Depth = d
			var pv struct {
				CP   *int   `json:"cp"`
				Mate *int   `json:"mate"`
				Line string `json:"line"`
			}
			pv.Line = s[1]
			ev.PVs = append(ev.PVs, pv)
			e.Evals = append(e.Evals, ev)
		}
		return e
	}
	// Depths out of order: the deepest wins, and a shallower one after it
	// does not overwrite it.
	e := line([2]string{"40", "d2d4 d7d5"}, [2]string{"20", "e2e4 e7e5"})
	if pv, ok := e.bestPV(); !ok || pv[0] != "d2d4" {
		t.Errorf("bestPV took %v %v", pv, ok)
	}
	if mv, ok := e.bestLine(); !ok || mv != "d2d4" {
		t.Errorf("bestLine took %q %v", mv, ok)
	}
	// The deepest entry is unusable, so the next deepest is taken.
	e = line([2]string{"40", "   "}, [2]string{"20", "e2e4"})
	if pv, ok := e.bestPV(); !ok || pv[0] != "e2e4" {
		t.Errorf("empty deepest line: %v %v", pv, ok)
	}
	if mv, ok := e.bestLine(); !ok || mv != "e2e4" {
		t.Errorf("empty deepest line: %q %v", mv, ok)
	}
	// A first token too short to be a move is skipped by bestLine.
	e = line([2]string{"40", "e2 e7e5"}, [2]string{"20", "g1f3"})
	if mv, ok := e.bestLine(); !ok || mv != "g1f3" {
		t.Errorf("short token: %q %v", mv, ok)
	}
	// One entry with no principal variation at all.
	var empty evalLine
	empty.Evals = append(empty.Evals, struct {
		PVs []struct {
			CP   *int   `json:"cp"`
			Mate *int   `json:"mate"`
			Line string `json:"line"`
		} `json:"pvs"`
		Depth int `json:"depth"`
	}{Depth: 30})
	if _, ok := empty.bestPV(); ok {
		t.Error("a record with no PV produced one")
	}
	if _, ok := empty.bestLine(); ok {
		t.Error("a record with no PV produced a line")
	}
}

// The book walker's rejections: a principal variation whose moves are not
// UCI, are illegal in the position, or run past the requested maximum.
func TestExtractBookRejectsUnusableVariations(t *testing.T) {
	dir := t.TempDir()
	const start = `"fen":"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq -"`
	dump := "{" + start + `,"evals":[{"pvs":[{"cp":20,"line":"zzzz"}],"depth":30}]}` + "\n" +
		"{" + start + `,"evals":[{"pvs":[{"cp":20,"line":"a1a8"}],"depth":30}]}` + "\n" +
		`{"fen":"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq -","evals":[{"pvs":[{"cp":20,"line":"e2e4 e7e5 g1f3 b8c6 f1b5"}],"depth":30}]}` + "\n"
	out := filepath.Join(dir, "book.txt")
	if err := ExtractBook(strings.NewReader(dump), out, 0, 4); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), "|e2e4") {
		t.Errorf("the legal variation was not written: %q", data)
	}
	if strings.Contains(string(data), "zzzz") || strings.Contains(string(data), "a1a8") {
		t.Errorf("an unusable move was written: %q", data)
	}
	// A maximum stops the walk part-way through a variation.
	capped := filepath.Join(dir, "capped.txt")
	if err := ExtractBook(strings.NewReader(dump), capped, 2, 4); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(mustRead(t, capped)), "\n"); n > 2 {
		t.Errorf("a cap of 2 wrote %d lines", n)
	}
}

// A record whose FEN does not parse, and one seen twice, are both skipped
// by the openings extractor.
func TestExtractOpeningsSkipsUnparsableAndRepeated(t *testing.T) {
	dir := t.TempDir()
	dump := `{"fen":"not/a/board w - -","evals":[{"pvs":[{"cp":1,"line":"e2e4"}],"depth":30}]}` + "\n" +
		`{"fen":"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq -","evals":[{"pvs":[{"cp":1,"line":"e2e4"}],"depth":30}]}` + "\n" +
		`{"fen":"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq -","evals":[{"pvs":[{"cp":1,"line":"d2d4"}],"depth":30}]}` + "\n"
	out := filepath.Join(dir, "openings.txt")
	if err := ExtractOpenings(strings.NewReader(dump), out, 0, 4); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(mustRead(t, out)), "\n"); n != 1 {
		t.Errorf("wrote %d openings, want 1 (one unparsable, one repeat)", n)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
