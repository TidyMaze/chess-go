// Pool record decoding, shared in spirit with cmd/poolaudit. Duplicated
// rather than extracted: both are package main commands, and a third
// package for eighty lines of struct and bit-twiddling would cost more to
// follow than it saves.
package main

import (
	"encoding/binary"
	"math"
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
