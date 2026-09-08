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
