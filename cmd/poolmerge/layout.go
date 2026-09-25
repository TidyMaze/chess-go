package main

import (
	"encoding/json"
	"fmt"
	"os"

	"chess/pool"
)

// layout is what a sidecar says about a pool's feature indices: under
// another bucket count or mirror, the same index names another square.
type layout struct {
	Buckets    int  `json:"buckets"`
	KingMirror bool `json:"king_mirror"`
}

// sameLayout is the one layout every source shares, or an error naming two
// that differ. A pool without a sidecar counts as unmirrored, as the
// trainer reads it; buckets 0 is a sidecar that does not know, and matches.
func sameLayout(paths []string) (layout, error) {
	var out layout
	var first, bucketsFrom string
	for i, p := range paths {
		l, err := readLayout(p)
		if err != nil {
			return layout{}, err
		}
		if i == 0 {
			out.KingMirror, first = l.KingMirror, p
		} else if l.KingMirror != out.KingMirror {
			return layout{}, fmt.Errorf("%s has king_mirror %v and %s has %v: merging them mixes two feature layouts",
				first, out.KingMirror, p, l.KingMirror)
		}
		switch {
		case l.Buckets == 0:
		case out.Buckets == 0:
			out.Buckets, bucketsFrom = l.Buckets, p
		case l.Buckets != out.Buckets:
			return layout{}, fmt.Errorf("%s has %d king buckets and %s has %d: merging them mixes two feature layouts",
				bucketsFrom, out.Buckets, p, l.Buckets)
		}
	}
	return out, nil
}

func readLayout(path string) (layout, error) {
	data, err := os.ReadFile(pool.MetaPath(path))
	if os.IsNotExist(err) {
		return layout{}, nil
	}
	if err != nil {
		return layout{}, err
	}
	var l layout
	if err := json.Unmarshal(data, &l); err != nil {
		return layout{}, fmt.Errorf("%s: %w", pool.MetaPath(path), err)
	}
	return l, nil
}

// writeMergedProvenance writes the merged sidecar with the king_mirror key,
// which pool.Provenance has no field for.
func writeMergedProvenance(out string, pv pool.Provenance, mirror bool) error {
	data, err := json.Marshal(pv)
	if err != nil {
		return err
	}
	meta := map[string]any{}
	if err := json.Unmarshal(data, &meta); err != nil {
		return err
	}
	meta["king_mirror"] = mirror
	if data, err = json.MarshalIndent(meta, "", "  "); err != nil {
		return err
	}
	return os.WriteFile(pool.MetaPath(out), append(data, '\n'), 0o644)
}
