// Command poolaudit reports whether a training pool is worth training on:
// how many distinct positions it holds, how its labels are spread between
// winning, level and losing, which phases of the game it covers, and how
// many lines it saw rather than how many positions it stored.
//
// The questions it answers are the ones that decide whether a network can
// learn from it at all. A pool of a million positions that are mostly the
// same opening, mostly level, and mostly from move ten teaches a network
// to be right about move ten and nothing else.
package main

import (
	"encoding/binary"
	"math"
	"sort"
)

// record is one stored position: the game it came from, the label, and the
// active features from each side's point of view.
type record struct {
	game   int32
	target float32
	static float32
	own    []uint16
	opp    []uint16
}

// recordSize is the fixed part: game, target, static, two lengths.
const recordSize = 14

// decode walks the pool bytes. A truncated tail is a crash mid-write
// rather than corruption, so it stops rather than failing.
func decode(data []byte, fn func(record) bool) int {
	pos, n := 0, len(data)
	seen := 0
	for pos+recordSize <= n {
		r := record{
			game:   int32(binary.LittleEndian.Uint32(data[pos:])),
			target: math.Float32frombits(binary.LittleEndian.Uint32(data[pos+4:])),
			static: math.Float32frombits(binary.LittleEndian.Uint32(data[pos+8:])),
		}
		nOwn, nOpp := int(data[pos+12]), int(data[pos+13])
		pos += recordSize
		if pos+2*(nOwn+nOpp) > n {
			break
		}
		r.own = make([]uint16, nOwn)
		r.opp = make([]uint16, nOpp)
		for i := 0; i < nOwn; i++ {
			r.own[i] = binary.LittleEndian.Uint16(data[pos+2*i:])
		}
		for i := 0; i < nOpp; i++ {
			r.opp[i] = binary.LittleEndian.Uint16(data[pos+2*(nOwn+i):])
		}
		pos += 2 * (nOwn + nOpp)
		seen++
		if !fn(r) {
			break
		}
	}
	return seen
}

// men is the number of pieces on the board. Every non-king piece
// contributes exactly one feature per perspective, and kings contribute
// none: they are what the features are indexed by.
func men(r record) int { return len(r.own) + 2 }

// phase names the position by how much material is left, on the same
// boundaries the evaluation uses.
func phase(men int) string {
	switch {
	case men >= 26:
		return "opening"
	case men >= 16:
		return "middlegame"
	default:
		return "endgame"
	}
}

// labelBand buckets a target so the spread between decisive and level
// positions is visible. A pool with nothing past a pawn teaches a network
// only how to tell a draw from a draw.
func labelBand(t float64) string {
	a := math.Abs(t)
	switch {
	case a < 0.3:
		return "level (<0.3)"
	case a < 1:
		return "slight (0.3-1)"
	case a < 3:
		return "clear (1-3)"
	case a < 6:
		return "winning (3-6)"
	default:
		return "decisive (6+)"
	}
}

// positionKey identifies a position by its features, so repeats across
// games can be counted. Both perspectives are included: the same men on
// the same squares with the kings elsewhere is a different position.
func positionKey(r record) uint64 {
	const (
		offset = 1469598103934665603
		prime  = 1099511628211
	)
	h := uint64(offset)
	mix := func(v uint16) {
		h ^= uint64(v)
		h *= prime
	}
	for _, v := range r.own {
		mix(v)
	}
	mix(0xffff)
	for _, v := range r.opp {
		mix(v)
	}
	return h
}

// share formats a count as a percentage of a total.
func share(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}

// sortedByCount returns the keys of m ordered by descending count.
func sortedByCount(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return m[out[i]] > m[out[j]] })
	return out
}
