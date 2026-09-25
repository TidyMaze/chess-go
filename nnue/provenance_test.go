package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"chess/engine"
	"chess/pool"
)

// A pool the generator writes must say what produced it. Five pools on disk
// needed their provenance reconstructed by hand because nothing recorded it
// while the positions were being written.
func TestSelfPlayPoolRecordsHowItWasGenerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gen.bin")
	if err := recordSelfPlayProvenance(path, 6, 8, 0.8, 8, false, 0.35); err != nil {
		t.Fatal(err)
	}
	got, ok := pool.ReadProvenance(path)
	if !ok {
		t.Fatal("the generator wrote no provenance beside its pool")
	}
	if got.Positions != "self-play" || got.Labeller != "self" {
		t.Errorf("self-play positions labelled by this engine, got %+v", got)
	}
	if got.LabelDepth != 8 || got.Lambda != 0.8 || got.Buckets != 8 {
		t.Errorf("label depth, lambda and buckets must survive, got %+v", got)
	}
}

// -king-mirror makes generation write mirrored features directly and says so
// in the pool meta; without it the same games give today's features.
func TestRunImportPGNWithKingMirror(t *testing.T) {
	dir := inScratch(t)
	t.Cleanup(func() { engine.FeatureKingBuckets, engine.FeatureKingMirror = 8, false })
	pgn := filepath.Join(dir, "g.pgn")
	os.WriteFile(pgn, []byte(operaGamePGN), 0o600)
	plain, mirrored := filepath.Join(dir, "plain.bin"), filepath.Join(dir, "mirrored.bin")
	for _, c := range []struct {
		pool  string
		extra []string
	}{{plain, nil}, {mirrored, []string{"-king-mirror"}}} {
		args := append([]string{"-import-pgn", pgn, "-pool-file", c.pool, "-import-max", "20",
			"-label-depth", "1", "-pgn-skip-plies", "4", "-king-buckets", "8"}, c.extra...)
		if code := run(args); code != 0 {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
	if _, err := os.Stat(pool.MetaPath(plain)); !os.IsNotExist(err) {
		t.Errorf("an unmirrored import must not start writing a meta, stat says %v", err)
	}
	data, err := os.ReadFile(pool.MetaPath(mirrored))
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil || meta["king_mirror"] != true {
		t.Errorf("mirrored import meta %s (%v), want king_mirror true", data, err)
	}
	p, errP := loadPool(plain, 0)
	m, errM := loadPool(mirrored, 0)
	if errP != nil || errM != nil || len(p) == 0 || len(p) != len(m) {
		t.Fatalf("pools: %d and %d positions (%v, %v)", len(p), len(m), errP, errM)
	}
	differ := 0
	for i := range p {
		if !slices.Equal(p[i].opp, m[i].opp) {
			differ++
		}
	}
	if differ == 0 {
		t.Error("black's king sits on e8, yet no Black-perspective feature list changed under -king-mirror")
	}
}

// A mirrored pool holds different features at the same bucket count, so its
// meta must say so without losing what the generator already recorded.
func TestMirroredPoolSaysSoInItsMeta(t *testing.T) {
	dir := t.TempDir()
	gen := filepath.Join(dir, "gen.bin")
	if err := recordSelfPlayProvenance(gen, 6, 8, 0.8, 8, false, 0.35); err != nil {
		t.Fatal(err)
	}
	imported := filepath.Join(dir, "imported.bin")
	for _, path := range []string{gen, imported} {
		if err := recordKingMirror(path, 8); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(pool.MetaPath(path))
		if err != nil {
			t.Fatal(err)
		}
		var meta map[string]any
		if err := json.Unmarshal(data, &meta); err != nil {
			t.Fatal(err)
		}
		if meta["king_mirror"] != true || meta["buckets"] != 8.0 {
			t.Errorf("%s: meta must record 8 buckets and king_mirror, got %v", path, meta)
		}
	}
	if got, _ := pool.ReadProvenance(gen); got.Positions != "self-play" || got.LabelDepth != 8 {
		t.Errorf("recording the mirror lost the generator's provenance, got %+v", got)
	}
}
