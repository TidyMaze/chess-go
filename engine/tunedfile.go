package engine

import (
	"encoding/json"
	"fmt"
	"os"
)

// A TunedFile is a parameter set produced by the tuner, loaded at runtime
// rather than compiled in.
//
// The compiled-in set in tuned.go exists to record a finding, not to be
// played. Loading from a file is what lets a new fit be raced without
// editing and rebuilding the engine, which matters because every fit so
// far has improved its objective and lost Elo, and the only way to know
// which way a new one goes is to play it.
type TunedFile struct {
	Material  []float64 `json:"material"`
	Mobility  []float64 `json:"mobility"`
	PSTScale  []float64 `json:"pst_scale"`
	Structure struct {
		Doubled       float64 `json:"Doubled"`
		Isolated      float64 `json:"Isolated"`
		PassedBase    float64 `json:"PassedBase"`
		PassedPerRank float64 `json:"PassedPerRank"`
		RookOpen      float64 `json:"RookOpen"`
		RookSemiOpen  float64 `json:"RookSemiOpen"`
		KingShield    float64 `json:"KingShield"`
	} `json:"structure"`
}

func LoadTunedFile(path string) (*TunedFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t TunedFile
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	if len(t.Material) != 6 || len(t.Mobility) != 6 || len(t.PSTScale) != 6 {
		return nil, fmt.Errorf("tuned file %s: want 6 entries per array, got %d/%d/%d",
			path, len(t.Material), len(t.Mobility), len(t.PSTScale))
	}
	return &t, nil
}

// Apply sets the fitted parameters on a player. Everything the fit
// produced is applied together: the values are only correct as a set,
// since each was fitted with the others present.
func (t *TunedFile) Apply(p *Player) {
	if t == nil {
		return
	}
	var w [6]float64
	copy(w[:], t.Material)
	p.Weights = &w

	var mob [6]float64
	copy(mob[:], t.Mobility)
	p.Mobility, p.MobilityW = true, &mob

	var pst [6]float64
	copy(pst[:], t.PSTScale)
	p.PSTScale = &pst

	sw := DefaultStructureWeights()
	sw.Doubled = t.Structure.Doubled
	sw.Isolated = t.Structure.Isolated
	sw.PassedBase = t.Structure.PassedBase
	sw.PassedPerRank = t.Structure.PassedPerRank
	sw.RookOpen = t.Structure.RookOpen
	sw.RookSemiOpen = t.Structure.RookSemiOpen
	sw.KingShield = t.Structure.KingShield
	p.StructureW = &sw

	// A fitted set replaces the compiled-in one rather than stacking with
	// it; setting both would apply two different fits at once.
	p.Tuned = false
}
