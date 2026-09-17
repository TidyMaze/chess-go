package main

import (
	"path/filepath"
	"testing"
)

func TestProvenanceRoundTrips(t *testing.T) {
	dir := t.TempDir()
	pool := filepath.Join(dir, "p.bin")
	want := Provenance{Positions: "pgn", Source: "lichess_games_2015-01.pgn.zst",
		Labeller: "self", LabelDepth: 3, Lambda: 0.8, Buckets: 8, Note: "skip 8 plies, quiet only"}
	if err := WriteProvenance(pool, want); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadProvenance(pool)
	if !ok {
		t.Fatal("wrote provenance and could not read it back")
	}
	if got != want {
		t.Errorf("round trip changed it:\n got %+v\nwant %+v", got, want)
	}
}

// A pool written before provenance existed must read as "unknown" rather
// than as a wrong answer: there are gigabytes of those on disk.
func TestAPoolWithoutProvenanceIsNotGuessed(t *testing.T) {
	if _, ok := ReadProvenance(filepath.Join(t.TempDir(), "nothing.bin")); ok {
		t.Error("a pool with no sidecar reported provenance anyway")
	}
}
