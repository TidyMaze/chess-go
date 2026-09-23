package engine

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// SearchTune overrides the search's hand-set pruning and reduction
// constants, one field per formula, so an SPSA tuner can race two
// players with different values in one process. Nil, on Eval or Player,
// means the historical defaults DefaultSearchTune returns, and every
// call site below falls back to the untuned literal in that case, so a
// search with no override is bit-identical to one that never heard of
// SearchTune.
type SearchTune struct {
	// RFPBase, RFPSlope: reverseFutilityMargin, search2.go.
	RFPBase, RFPSlope float64
	// RazorBase, RazorSlope: razorMargin, razor.go.
	RazorBase, RazorSlope float64
	// FutilityPerDepth: futilityMargin, search2.go.
	FutilityPerDepth float64
	// LMRBase, LMRDiv: the lmrReductions table, search2.go.
	LMRBase, LMRDiv float64
	// LMPBase: lateMovePrunedMax's move-count threshold, search2.go.
	LMPBase float64
	// NullBase, NullDepthDiv, NullBonusPer: nullMoveReduction, search2.go.
	NullBase     float64
	NullDepthDiv float64
	NullBonusPer float64
	// AspDelta: the aspiration window half-width, search2.go.
	AspDelta float64
	// DeltaMargin: quiescence delta pruning's safety margin, search.go.
	DeltaMargin float64
}

// DefaultSearchTune is today's hand-set literals, unchanged.
func DefaultSearchTune() SearchTune {
	return SearchTune{
		RFPBase: 0.5, RFPSlope: 0.35,
		RazorBase: 2.0, RazorSlope: 1.5,
		FutilityPerDepth: 1.0,
		LMRBase:          0.5,
		LMRDiv:           2.5,
		LMPBase:          3,
		NullBase:         3,
		NullDepthDiv:     4,
		NullBonusPer:     1.5,
		AspDelta:         0.5,
		DeltaMargin:      2.0,
	}
}

// searchTuneFields is the name -> field table ParseSearchTune and String
// share, so parsing and printing a SearchTune can never drift apart.
var searchTuneFields = []struct {
	name string
	get  func(*SearchTune) float64
	set  func(*SearchTune, float64)
}{
	{"RFPBase", func(t *SearchTune) float64 { return t.RFPBase }, func(t *SearchTune, v float64) { t.RFPBase = v }},
	{"RFPSlope", func(t *SearchTune) float64 { return t.RFPSlope }, func(t *SearchTune, v float64) { t.RFPSlope = v }},
	{"RazorBase", func(t *SearchTune) float64 { return t.RazorBase }, func(t *SearchTune, v float64) { t.RazorBase = v }},
	{"RazorSlope", func(t *SearchTune) float64 { return t.RazorSlope }, func(t *SearchTune, v float64) { t.RazorSlope = v }},
	{"FutilityPerDepth", func(t *SearchTune) float64 { return t.FutilityPerDepth }, func(t *SearchTune, v float64) { t.FutilityPerDepth = v }},
	{"LMRBase", func(t *SearchTune) float64 { return t.LMRBase }, func(t *SearchTune, v float64) { t.LMRBase = v }},
	{"LMRDiv", func(t *SearchTune) float64 { return t.LMRDiv }, func(t *SearchTune, v float64) { t.LMRDiv = v }},
	{"LMPBase", func(t *SearchTune) float64 { return t.LMPBase }, func(t *SearchTune, v float64) { t.LMPBase = v }},
	{"NullBase", func(t *SearchTune) float64 { return t.NullBase }, func(t *SearchTune, v float64) { t.NullBase = v }},
	{"NullDepthDiv", func(t *SearchTune) float64 { return t.NullDepthDiv }, func(t *SearchTune, v float64) { t.NullDepthDiv = v }},
	{"NullBonusPer", func(t *SearchTune) float64 { return t.NullBonusPer }, func(t *SearchTune, v float64) { t.NullBonusPer = v }},
	{"AspDelta", func(t *SearchTune) float64 { return t.AspDelta }, func(t *SearchTune, v float64) { t.AspDelta = v }},
	{"DeltaMargin", func(t *SearchTune) float64 { return t.DeltaMargin }, func(t *SearchTune, v float64) { t.DeltaMargin = v }},
}

// ParseSearchTune parses "Name=value,Name=value" overrides on top of
// DefaultSearchTune. An empty string means no override at all: nil, nil.
// An unknown field name is an error.
func ParseSearchTune(s string) (*SearchTune, error) {
	if s == "" {
		return nil, nil
	}
	t := DefaultSearchTune()
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("searchtune: %q is not name=value", part)
		}
		name = strings.TrimSpace(name)
		v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, fmt.Errorf("searchtune: %s: %w", name, err)
		}
		found := false
		for _, f := range searchTuneFields {
			if f.name == name {
				f.set(&t, v)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("searchtune: unknown field %q", name)
		}
	}
	return &t, nil
}

// String renders every field as "Name=value,...", in the order
// searchTuneFields lists them, parseable back by ParseSearchTune.
func (t SearchTune) String() string {
	parts := make([]string, len(searchTuneFields))
	for i, f := range searchTuneFields {
		parts[i] = f.name + "=" + strconv.FormatFloat(f.get(&t), 'g', -1, 64)
	}
	return strings.Join(parts, ",")
}

// tune returns the effective SearchTune for this Eval; nil (including a
// nil Eval) means the defaults, read at each call site below.
func (e *Eval) tune() *SearchTune {
	if e == nil {
		return nil
	}
	return e.Tune
}

// marginRFP is reverseFutilityMargin, reading the tune when there is one.
func (t *SearchTune) marginRFP(depth int) float64 {
	if t == nil {
		return reverseFutilityMargin(depth)
	}
	return t.RFPBase + t.RFPSlope*float64(depth)
}

// cutsRFP is reverseFutilityCuts, reading the tune when there is one.
func (t *SearchTune) cutsRFP(depth int, staticEval, alpha, beta float64, maximizing, inCheck bool) bool {
	const mateBound = mateScore - maxSearchPly
	if depth < 4 || depth > 7 || inCheck || !zeroWindow(alpha, beta) || alpha <= -mateBound || beta >= mateBound {
		return false
	}
	margin := t.marginRFP(depth)
	if maximizing {
		return staticEval-margin >= beta
	}
	return staticEval+margin <= alpha
}

// marginRazor is razorMargin, reading the tune when there is one.
func (t *SearchTune) marginRazor(depth int) float64 {
	if t == nil {
		return razorMargin(depth)
	}
	return t.RazorBase + t.RazorSlope*float64(depth)
}

// cutsRazor is razorCuts, reading the tune when there is one.
func (t *SearchTune) cutsRazor(depth int, staticEval, alpha, beta float64, maximizing bool) bool {
	if depth < 1 || depth > 3 {
		return false
	}
	margin := t.marginRazor(depth)
	if maximizing {
		return staticEval+margin <= alpha
	}
	return staticEval-margin >= beta
}

// marginFutility is futilityMargin[depth], reading the tune when there
// is one.
func (t *SearchTune) marginFutility(depth int) float64 {
	if t == nil {
		return futilityMargin[depth]
	}
	return t.FutilityPerDepth * float64(depth)
}

// reductionNull is nullMoveReduction, reading the tune when there is one.
//
// The two truncations (the base-plus-depth-share term, then the margin
// bonus) stay separate, matching the integer arithmetic the untuned
// formula does, so a default tune truncates to the exact same plies:
// proved for depth 0..64 by TestSearchTuneNullReductionMatchesDefault.
func (t *SearchTune) reductionNull(depth int, marginOverBound float64) int {
	if t == nil {
		return nullMoveReduction(depth, marginOverBound)
	}
	r := int(t.NullBase) + int(float64(depth)/t.NullDepthDiv)
	bonus := int(marginOverBound / t.NullBonusPer)
	if bonus > 2 {
		bonus = 2
	}
	if bonus > 0 {
		r += bonus
	}
	if r > depth {
		r = depth
	}
	return r
}

// prunedLMP is lateMovePrunedMax, reading the tune when there is one.
//
// Truncating LMPBase to an int before the integer division matches the
// untuned (3+depth*depth)/div exactly at the default LMPBase=3: proved
// for depth 0..64 by TestSearchTuneLMPMatchesDefault.
func (t *SearchTune) prunedLMP(depth, moveIndex int, improving, inCheck, isCapture, promoted, givesCheck, isKiller bool, maxDepth int) bool {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if depth > maxDepth || inCheck || isCapture || promoted || givesCheck || isKiller {
		return false
	}
	base := 3
	if t != nil {
		base = int(t.LMPBase)
	}
	div := 2
	if improving {
		div = 1
	}
	return moveIndex >= (base+depth*depth)/div
}

// aspirationDelta is the aspiration window half-width, reading the tune
// when there is one.
func (t *SearchTune) aspirationDelta() float64 {
	if t == nil {
		return 0.5
	}
	return t.AspDelta
}

// quiesceDeltaMargin is quiescence delta pruning's safety margin, reading
// the tune when there is one.
func (t *SearchTune) quiesceDeltaMargin() float64 {
	if t == nil {
		return 2.0
	}
	return t.DeltaMargin
}

// lmrValue looks up the LMR table entry for the searchCtx's tune,
// building a per-tune table lazily on first use and caching it on the
// ctx (never per node): a search with no override never builds one at
// all and reads the package's precomputed lmrReductions instead.
func (c *searchCtx) lmrValue(depth, moveIndex int) int {
	t := c.ev.tune()
	if t == nil {
		return lmrTable(depth, moveIndex)
	}
	if c.lmrTuned == nil {
		var tbl [64][64]int
		for d := 1; d < 64; d++ {
			for m := 1; m < 64; m++ {
				r := t.LMRBase + math.Log(float64(d))*math.Log(float64(m))/t.LMRDiv
				tbl[d][m] = int(r)
			}
		}
		c.lmrTuned = &tbl
	}
	d, m := depth, moveIndex+1
	if d > 63 {
		d = 63
	}
	if m > 63 {
		m = 63
	}
	return c.lmrTuned[d][m]
}

// lmrReduction is lmrReduction (the package function), reading the ctx's
// tune when there is one instead of the fixed table.
func (c *searchCtx) lmrReduction(depth, moveIndex int, pvNode, isKiller, promoted bool) int {
	if depth < 3 || promoted {
		return 0
	}
	r := c.lmrValue(depth, moveIndex)
	if pvNode {
		r = r * 2 / 3
	} else if r > 1 {
		r++
	}
	if isKiller {
		r--
	}
	if r > depth-2 {
		r = depth - 2
	}
	if r < 1 {
		if isKiller {
			return 0
		}
		r = 1
	}
	return r
}
