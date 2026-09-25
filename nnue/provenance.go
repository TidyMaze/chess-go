package main

import (
	"encoding/json"
	"fmt"
	"os"

	"chess/pool"
)

// checkPoolLayout refuses a pool whose features were written under another
// king layout than this run's: appending would give one index two meanings.
// A pool without a sidecar counts as unmirrored, as the trainer reads it.
func checkPoolLayout(path string, buckets int, mirror bool) error {
	data, err := os.ReadFile(pool.MetaPath(path))
	if os.IsNotExist(err) {
		if st, statErr := os.Stat(path); mirror && statErr == nil && st.Size() > 0 {
			return fmt.Errorf("%s holds positions but has no %s, so they count as unmirrored; -king-mirror would add mirrored ones",
				path, pool.MetaPath(path))
		}
		return nil
	}
	if err != nil {
		return err
	}
	var meta struct {
		Buckets    int  `json:"buckets"`
		KingMirror bool `json:"king_mirror"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("%s: %w", pool.MetaPath(path), err)
	}
	// Buckets 0 is a sidecar that does not know, such as a merge of pools
	// that had none; only the mirror has a default, false.
	if (meta.Buckets != 0 && meta.Buckets != buckets) || meta.KingMirror != mirror {
		return fmt.Errorf("%s says buckets %d and king_mirror %v, this run writes buckets %d and king_mirror %v; use another -pool-file",
			pool.MetaPath(path), meta.Buckets, meta.KingMirror, buckets, mirror)
	}
	return nil
}

// recordKingMirror marks a pool's meta as holding left-right mirrored
// features, keeping whatever else the meta already says.
func recordKingMirror(path string, buckets int) error {
	meta := map[string]any{}
	if data, err := os.ReadFile(pool.MetaPath(path)); err == nil {
		if err := json.Unmarshal(data, &meta); err != nil {
			return fmt.Errorf("%s: %w", pool.MetaPath(path), err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	meta["buckets"] = buckets
	meta["king_mirror"] = true
	return writeMeta(path, meta)
}

// recordSelfPlayProvenance writes, beside a pool this run is filling, the
// settings that decide what its positions are worth.
//
// The pool format is a bare sequence of records with nowhere to put this,
// and the five pools that predate the sidecar each cost an evening of
// reconstruction: which of them held human games, at what depth they were
// labelled, and whether anything external had scored them.
func recordSelfPlayProvenance(path string, playDepth, labelDepth int, lambda float64, buckets int, mirror bool, quietTol float64) error {
	data, err := json.Marshal(pool.Provenance{
		Positions:  "self-play",
		Source:     fmt.Sprintf("self-play at depth %d by this engine", playDepth),
		Labeller:   "self",
		LabelDepth: labelDepth,
		Lambda:     lambda,
		Buckets:    buckets,
		Note: fmt.Sprintf("quiet tolerance %.2f pawns of static/quiescence disagreement",
			quietTol),
	})
	if err != nil {
		return err
	}
	meta := map[string]any{}
	if err := json.Unmarshal(data, &meta); err != nil {
		return err
	}
	// In the same write as the rest, so no crash leaves a mirrored pool
	// whose sidecar has lost the key.
	meta["king_mirror"] = mirror
	return writeMeta(path, meta)
}

func writeMeta(path string, meta map[string]any) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pool.MetaPath(path), append(data, '\n'), 0o644)
}
