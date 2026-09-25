package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"chess/engine"
	"chess/pool"
)

// tinySelfPlay is one generation of self-play small enough for a test.
var tinySelfPlay = []string{"-generations", "1", "-games", "2", "-play-depth", "1", "-label-depth", "1",
	"-eval-games", "2", "-eval-every", "1", "-eval-depth", "1", "-eval-blend", "0.3", "-max-plies", "20",
	"-hidden", "4", "-epochs", "1"}

func resetFeatureLayout(t *testing.T) {
	t.Cleanup(func() { engine.FeatureKingBuckets, engine.FeatureKingMirror = 8, false })
}

func readMeta(t *testing.T, poolPath string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(pool.MetaPath(poolPath))
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("%s: %v", pool.MetaPath(poolPath), err)
	}
	return meta
}

// readOrNil is a file's bytes, or nil when it does not exist.
func readOrNil(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return data
}

// writingModes are the three ways nnue-bin appends positions to -pool-file.
func writingModes(t *testing.T, dir string) []struct {
	name string
	args []string
} {
	pgn := filepath.Join(dir, "g.pgn")
	dump := filepath.Join(dir, "dump.jsonl")
	if err := os.WriteFile(pgn, []byte(operaGamePGN), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dump, []byte(syntheticDump), 0o600); err != nil {
		t.Fatal(err)
	}
	return []struct {
		name string
		args []string
	}{
		{"import-pgn", []string{"-import-pgn", pgn, "-import-max", "5", "-label-depth", "1", "-pgn-skip-plies", "4"}},
		{"import", []string{"-import", dump}},
		{"self-play", tinySelfPlay},
	}
}

// A 64-slot pool is the raw, unmirrored source rebucket.py derives b8m and
// b32m from, so a mirrored one is refused before anything is written.
func TestRunRefusesKingMirrorWith64Buckets(t *testing.T) {
	dir := inScratch(t)
	resetFeatureLayout(t)
	for _, mode := range writingModes(t, dir) {
		p := filepath.Join(dir, mode.name+".bin")
		args := append([]string{"-pool-file", p, "-king-buckets", "64", "-king-mirror"}, mode.args...)
		if code := run(args); code == 0 {
			t.Errorf("%s: -king-buckets 64 -king-mirror exited 0", mode.name)
		}
		for _, path := range []string{p, pool.MetaPath(p)} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("%s: %s was written (stat %v)", mode.name, path, err)
			}
		}
	}
}

// Positions of one layout appended to a pool of another make a file whose
// halves mean different things, and nothing downstream can tell them apart.
func TestRunRefusesToAppendUnderAnotherLayout(t *testing.T) {
	dir := inScratch(t)
	resetFeatureLayout(t)
	cases := []struct {
		name  string
		meta  string // "" is no sidecar
		flags []string
	}{
		{"mirrored pool, plain run", `{"buckets": 8, "king_mirror": true}`, nil},
		{"plain pool, mirrored run", `{"buckets": 8, "king_mirror": false}`, []string{"-king-mirror"}},
		{"pool silent on the mirror, mirrored run", `{"buckets": 8}`, []string{"-king-mirror"}},
		{"32-bucket pool, 8-bucket run", `{"buckets": 32, "king_mirror": false}`, nil},
		{"pool without a sidecar, mirrored run", "", []string{"-king-mirror"}},
	}
	for _, mode := range writingModes(t, dir) {
		for _, c := range cases {
			t.Run(mode.name+"/"+c.name, func(t *testing.T) {
				p := filepath.Join(t.TempDir(), "p.bin")
				if err := appendPool(p, []sample{{own: []int32{1, 2}, opp: []int32{3}}}); err != nil {
					t.Fatal(err)
				}
				if c.meta != "" {
					if err := os.WriteFile(pool.MetaPath(p), []byte(c.meta), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				poolBefore, metaBefore := readOrNil(t, p), readOrNil(t, pool.MetaPath(p))
				args := append(append([]string{"-pool-file", p, "-king-buckets", "8"}, mode.args...), c.flags...)
				if code := run(args); code == 0 {
					t.Error("exited 0")
				}
				if !bytes.Equal(readOrNil(t, p), poolBefore) {
					t.Error("the pool was appended to")
				}
				if !bytes.Equal(readOrNil(t, pool.MetaPath(p)), metaBefore) {
					t.Errorf("the sidecar was rewritten to %s", readOrNil(t, pool.MetaPath(p)))
				}
			})
		}
	}
}

// The check refuses only a disagreement: a pool of the same layout, or a
// legacy one with nothing to say, is appended to as before.
func TestRunAppendsToAPoolOfTheSameLayout(t *testing.T) {
	dir := inScratch(t)
	resetFeatureLayout(t)
	pgn := writingModes(t, dir)[0].args
	for _, c := range []struct {
		name  string
		meta  string
		flags []string
	}{
		{"mirrored", `{"buckets": 8, "king_mirror": true}`, []string{"-king-mirror"}},
		{"plain", `{"buckets": 8, "king_mirror": false}`, nil},
		{"silent on the mirror", `{"buckets": 8}`, nil},
		{"no sidecar", "", nil},
	} {
		p := filepath.Join(t.TempDir(), "p.bin")
		if err := appendPool(p, []sample{{own: []int32{1, 2}, opp: []int32{3}}}); err != nil {
			t.Fatal(err)
		}
		if c.meta != "" {
			if err := os.WriteFile(pool.MetaPath(p), []byte(c.meta), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		before := len(readOrNil(t, p))
		if code := run(append(append([]string{"-pool-file", p, "-king-buckets", "8"}, pgn...), c.flags...)); code != 0 {
			t.Errorf("%s: exit %d", c.name, code)
		}
		if len(readOrNil(t, p)) <= before {
			t.Errorf("%s: nothing was appended", c.name)
		}
	}
}

// Every sidecar says whether its pool is mirrored, false included: the
// trainer reads the key and exports it into the network the engine plays.
func TestSelfPlayMetaRecordsTheKingLayout(t *testing.T) {
	for _, mirror := range []bool{false, true} {
		inScratch(t)
		resetFeatureLayout(t)
		args := tinySelfPlay
		if mirror {
			args = append([]string{"-king-mirror"}, args...)
		}
		if code := run(append([]string{"-king-buckets", "8"}, args...)); code != 0 {
			t.Fatalf("mirror %v: exit %d", mirror, code)
		}
		meta := readMeta(t, "nnue_pool.bin")
		if meta["king_mirror"] != mirror || meta["buckets"] != 8.0 || meta["positions"] != "self-play" {
			t.Errorf("mirror %v: meta %v", mirror, meta)
		}
	}
}
