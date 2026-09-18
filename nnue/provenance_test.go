package main

import (
	"path/filepath"
	"testing"

	"chess/pool"
)

// A pool the generator writes must say what produced it. Five pools on disk
// needed their provenance reconstructed by hand because nothing recorded it
// while the positions were being written.
func TestSelfPlayPoolRecordsHowItWasGenerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gen.bin")
	if err := recordSelfPlayProvenance(path, 6, 8, 0.8, 8, 0.35); err != nil {
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
