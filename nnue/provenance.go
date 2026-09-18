package main

import (
	"fmt"

	"chess/pool"
)

// recordSelfPlayProvenance writes, beside a pool this run is filling, the
// settings that decide what its positions are worth.
//
// The pool format is a bare sequence of records with nowhere to put this,
// and the five pools that predate the sidecar each cost an evening of
// reconstruction: which of them held human games, at what depth they were
// labelled, and whether anything external had scored them.
func recordSelfPlayProvenance(path string, playDepth, labelDepth int, lambda float64, buckets int, quietTol float64) error {
	return pool.WriteProvenance(path, pool.Provenance{
		Positions:  "self-play",
		Source:     fmt.Sprintf("self-play at depth %d by this engine", playDepth),
		Labeller:   "self",
		LabelDepth: labelDepth,
		Lambda:     lambda,
		Buckets:    buckets,
		Note: fmt.Sprintf("quiet tolerance %.2f pawns of static/quiescence disagreement",
			quietTol),
	})
}
