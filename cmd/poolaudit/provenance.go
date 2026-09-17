package main

import (
	"encoding/json"
	"os"
)

// Provenance is what a pool file cannot say about itself.
//
// The format is a bare sequence of records: no header, no version, no room
// for one. Answering "where did these positions come from and who labelled
// them" meant reading both producers and counting game ids, which is not
// something the next person should have to redo, and is easy to get wrong.
//
// It is a sidecar rather than a header on purpose. There are 1.3 GB of
// pools on disk and four readers across two languages; a header would make
// every existing file unreadable by every existing reader, and the records
// have no spare field to hide one in. A sidecar costs nothing and can be
// backfilled for files written before it existed.
type Provenance struct {
	// Positions is where the board positions came from: "pgn" for a games
	// database, "selfplay" for the engine's own games.
	Positions string `json:"positions"`
	// Source names the file or run they came from.
	Source string `json:"source,omitempty"`
	// Labeller is what produced the target. "self" is this engine's own
	// search, which is the only thing the project's rules allow; anything
	// else has to be justified where it is set.
	Labeller string `json:"labeller"`
	// LabelDepth is the search depth behind each target.
	LabelDepth int `json:"label_depth,omitempty"`
	// Lambda is the weight on the search score against the game result.
	Lambda float64 `json:"lambda,omitempty"`
	// Buckets is the king-bucket scheme the feature indices assume. Mixing
	// two schemes trains on nonsense; poolmerge and the trainer both refuse.
	Buckets int `json:"buckets"`
	// Note is anything a reader would otherwise have to work out.
	Note string `json:"note,omitempty"`
}

// metaPath is where a pool's provenance lives.
func metaPath(pool string) string { return pool + ".meta.json" }

// WriteProvenance saves a pool's provenance beside it.
func WriteProvenance(pool string, p Provenance) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(pool), append(b, '\n'), 0o644)
}

// ReadProvenance loads a pool's provenance, reporting whether there was any.
func ReadProvenance(pool string) (Provenance, bool) {
	b, err := os.ReadFile(metaPath(pool))
	if err != nil {
		return Provenance{}, false
	}
	var p Provenance
	if json.Unmarshal(b, &p) != nil {
		return Provenance{}, false
	}
	return p, true
}
