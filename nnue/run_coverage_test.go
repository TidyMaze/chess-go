package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Every mode of the command, driven in-process with tiny inputs. The
// process changes into a scratch directory first, since several modes
// write status files where they run.
func inScratch(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	return dir
}

func TestRunRejectsBadFlags(t *testing.T) {
	if code := run([]string{"-no-such-flag"}); code != 2 {
		t.Errorf("bad flag exit %d", code)
	}
}

func TestRunBookFromPGNAndCountPool(t *testing.T) {
	dir := inScratch(t)
	pgn := filepath.Join(dir, "g.pgn")
	os.WriteFile(pgn, []byte(operaGamePGN), 0o600)
	book := filepath.Join(dir, "book.txt")
	if code := run([]string{"-book-from-pgn", book, "-book-pgn", pgn, "-book-plies", "6", "-book-min", "1"}); code != 0 {
		t.Errorf("book mode exit %d", code)
	}
	if code := run([]string{"-book-from-pgn", filepath.Join(dir, "no", "dir", "b"), "-book-pgn", pgn}); code == 0 {
		t.Error("unwritable book succeeded")
	}
	if code := run([]string{"-book-from-pgn", book, "-book-pgn", filepath.Join(dir, "absent.pgn")}); code == 0 {
		t.Error("missing PGN succeeded")
	}
	pool := filepath.Join(dir, "pool.bin")
	if code := run([]string{"-import-pgn", pgn, "-pool-file", pool, "-import-max", "20", "-label-depth", "1", "-pgn-skip-plies", "4"}); code != 0 {
		t.Errorf("import-pgn exit %d", code)
	}
	if code := run([]string{"-count-pool", pool}); code != 0 {
		t.Errorf("count-pool exit %d", code)
	}
	if code := run([]string{"-count-pool", filepath.Join(dir, "absent.bin")}); code == 0 {
		t.Error("counting a missing pool succeeded")
	}
}

func TestRunEmitEvalCheck(t *testing.T) {
	net, err := filepath.Abs("../champion_net.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(net); err != nil {
		t.Skip("no champion network")
	}
	dir := inScratch(t)
	if code := run([]string{"-emit-eval-check", filepath.Join(dir, "ec.json"), "-net-file", net}); code != 0 {
		t.Errorf("emit-eval-check exit %d", code)
	}
}

func TestRunTinyTrainingGeneration(t *testing.T) {
	inScratch(t)
	code := run([]string{"-generations", "1", "-games", "2", "-play-depth", "1", "-label-depth", "1",
		"-eval-games", "2", "-max-plies", "20"})
	if code != 0 {
		t.Errorf("tiny training run exit %d", code)
	}
}

func TestRunImportAndExtractModes(t *testing.T) {
	dir := inScratch(t)
	dump := filepath.Join(dir, "dump.jsonl")
	os.WriteFile(dump, []byte(syntheticDump), 0o600)
	pool := filepath.Join(dir, "pool.bin")
	if code := run([]string{"-import", dump, "-pool-file", pool, "-import-max", "3", "-import-quiet-filter"}); code != 0 {
		t.Errorf("import exit %d", code)
	}
	if code := run([]string{"-import", dump, "-pool-file", pool, "-import-resume"}); code != 0 {
		t.Errorf("import resume exit %d", code)
	}
	if code := run([]string{"-import", filepath.Join(dir, "absent.jsonl"), "-pool-file", pool}); code == 0 {
		t.Error("importing a missing file succeeded")
	}
	for _, mode := range []string{"-extract-openings", "-extract-book", "-extract-tuning"} {
		out := filepath.Join(dir, mode[1:]+".txt")
		if code := run([]string{"-import", dump, mode, out, "-opening-max", "5", "-opening-min-pieces", "4"}); code != 0 {
			t.Errorf("%s exit %d", mode, code)
		}
		if code := run([]string{"-import", dump, mode, filepath.Join(dir, "no", "dir", "x")}); code == 0 {
			t.Errorf("%s into a missing directory succeeded", mode)
		}
	}
	// The dump on standard input.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	go func() { w.Write([]byte(syntheticDump)); w.Close() }()
	code := run([]string{"-import", "-", "-pool-file", filepath.Join(dir, "stdin.bin")})
	os.Stdin = oldStdin
	if code != 0 {
		t.Errorf("import from stdin exit %d", code)
	}
}

func TestRunOpeningBookAndLabelChampion(t *testing.T) {
	dir := inScratch(t)
	book := filepath.Join(dir, "openings.txt")
	os.WriteFile(book, []byte("rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1\nrnbqkbnr/pppppppp/8/8/3P4/8/PPP1PPPP/RNBQKBNR b KQkq d3 0 1\n"), 0o600)
	tiny := []string{"-generations", "1", "-games", "2", "-play-depth", "1", "-label-depth", "1", "-eval-games", "2",
		"-eval-every", "1", "-eval-depth", "1", "-eval-blend", "0.3", "-max-plies", "20", "-hidden", "4", "-epochs", "1", "-king-buckets", "8"}
	if code := run(append([]string{"-opening-book", book}, tiny...)); code != 0 {
		t.Errorf("self-play from a book exit %d", code)
	}
	empty := filepath.Join(dir, "empty.txt")
	os.WriteFile(empty, nil, 0o600)
	run(append([]string{"-opening-book", empty}, tiny...))                        // reports an empty book
	run(append([]string{"-opening-book", filepath.Join(dir, "absent")}, tiny...)) // and a missing one
	// A second generation resumes from what the first left behind; -fresh and -fresh-pool ignore it.
	if code := run(tiny); code != 0 {
		t.Errorf("resume exit %d", code)
	}
	if code := run(append([]string{"-fresh", "-fresh-pool", "-target", "0.5", "-quiet", "0.3", "-smooth", "0.5", "-decay", "0.001"}, tiny...)); code != 0 {
		t.Errorf("fresh exit %d", code)
	}
	// Labelling PGN imports with a champion file.
	champ := filepath.Join(dir, "champion.json")
	os.WriteFile(champ, []byte(`{"label":"t","depth":1}`), 0o600)
	pgn := filepath.Join(dir, "g.pgn")
	os.WriteFile(pgn, []byte(operaGamePGN), 0o600)
	if code := run([]string{"-import-pgn", pgn, "-pool-file", filepath.Join(dir, "p.bin"), "-import-max", "10", "-label-depth", "1", "-label-champion", champ}); code != 0 {
		t.Errorf("import-pgn with a champion exit %d", code)
	}
}

func TestRunStockfishTeacher(t *testing.T) {
	if _, err := os.Stat("/opt/homebrew/bin/stockfish"); err != nil {
		t.Skip("no Stockfish")
	}
	inScratch(t)
	code := run([]string{"-teacher", "stockfish", "-teacher-depth", "1", "-generations", "1", "-games", "2",
		"-play-depth", "1", "-eval-games", "2", "-max-plies", "12", "-hidden", "4", "-epochs", "1"})
	if code != 0 {
		t.Errorf("stockfish teacher exit %d", code)
	}
}
