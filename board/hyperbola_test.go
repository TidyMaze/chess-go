package board

import (
	"math/bits"
	"math/rand"
	"testing"
)

// positiveDir says whether a direction's squares have higher indices than
// their origin, which decides whether the nearest blocker is the lowest set
// bit or the highest.
var positiveDir = [numDirs]bool{
	dirNorth: true, dirEast: true, dirNorthEast: true, dirNorthWest: true,
}

// rayFrom is the ray in direction d from sq, truncated at the first
// occupied square and including it: the classical method the slider
// attacks used before hyperbola quintessence.
func rayFrom(d int, sq uint8, occupied uint64) uint64 {
	attacks := rayAttacks[d][sq]
	blockers := attacks & occupied
	if blockers == 0 {
		return attacks
	}
	var first int
	if positiveDir[d] {
		first = bits.TrailingZeros64(blockers)
	} else {
		first = 63 - bits.LeadingZeros64(blockers)
	}
	return attacks &^ rayAttacks[d][first]
}

// rookAttacksByRays and bishopAttacksByRays are RookAttacks and
// BishopAttacks as they were before hyperbola quintessence: one ray per
// direction, each cut at its nearest blocker by a bit scan. They stay as
// the oracle for the branchless version.
func rookAttacksByRays(sq uint8, occ uint64) uint64 {
	return rayFrom(dirNorth, sq, occ) | rayFrom(dirSouth, sq, occ) |
		rayFrom(dirEast, sq, occ) | rayFrom(dirWest, sq, occ)
}

func bishopAttacksByRays(sq uint8, occ uint64) uint64 {
	return rayFrom(dirNorthEast, sq, occ) | rayFrom(dirNorthWest, sq, occ) |
		rayFrom(dirSouthEast, sq, occ) | rayFrom(dirSouthWest, sq, occ)
}

// randomOccupancy spreads the density from nearly empty to nearly full,
// since a slider's answer depends on where the nearest blockers are and a
// uniform 50% board rarely leaves a long open ray.
func randomOccupancy(rng *rand.Rand) uint64 {
	switch rng.Intn(4) {
	case 0:
		return rng.Uint64() & rng.Uint64() & rng.Uint64()
	case 1:
		return rng.Uint64() & rng.Uint64()
	case 2:
		return rng.Uint64()
	}
	return rng.Uint64() | rng.Uint64()
}

func TestLineMasksAreTheTwoOppositeRays(t *testing.T) {
	for sq := 0; sq < 64; sq++ {
		l := lineMasks[sq]
		cases := []struct {
			name      string
			got, want uint64
		}{
			{"file", l.file, rayAttacks[dirNorth][sq] | rayAttacks[dirSouth][sq]},
			{"rank", l.rank, rayAttacks[dirEast][sq] | rayAttacks[dirWest][sq]},
			{"diagonal", l.diag, rayAttacks[dirNorthEast][sq] | rayAttacks[dirSouthWest][sq]},
			{"anti-diagonal", l.anti, rayAttacks[dirNorthWest][sq] | rayAttacks[dirSouthEast][sq]},
		}
		for _, c := range cases {
			if c.got != c.want {
				t.Errorf("%s mask of %d = %#016x, want %#016x", c.name, sq, c.got, c.want)
			}
		}
	}
}

func TestLineAttacksMatchesTheRayPairs(t *testing.T) {
	rng := rand.New(rand.NewSource(21))
	for trial := 0; trial < 4000; trial++ {
		occ := randomOccupancy(rng)
		for i := 0; i < 64; i++ {
			sq := uint8(i)
			l := lineMasks[sq]
			cases := []struct {
				name   string
				mask   uint64
				d1, d2 int
			}{
				{"file", l.file, dirNorth, dirSouth},
				{"rank", l.rank, dirEast, dirWest},
				{"diagonal", l.diag, dirNorthEast, dirSouthWest},
				{"anti-diagonal", l.anti, dirNorthWest, dirSouthEast},
			}
			for _, c := range cases {
				got := lineAttacks(sq, occ, c.mask)
				want := rayFrom(c.d1, sq, occ) | rayFrom(c.d2, sq, occ)
				if got != want {
					t.Fatalf("%s from %d, occ %#016x: %#016x, want %#016x", c.name, sq, occ, got, want)
				}
			}
		}
	}
}

func TestSliderAttacksMatchTheRays(t *testing.T) {
	rng := rand.New(rand.NewSource(22))
	for trial := 0; trial < 4000; trial++ {
		occ := randomOccupancy(rng)
		if trial%10 == 0 {
			occ = 0
		}
		for i := 0; i < 64; i++ {
			sq := uint8(i)
			if got, want := RookAttacks(sq, occ), rookAttacksByRays(sq, occ); got != want {
				t.Fatalf("RookAttacks(%d, %#016x) = %#016x, want %#016x", sq, occ, got, want)
			}
			if got, want := BishopAttacks(sq, occ), bishopAttacksByRays(sq, occ); got != want {
				t.Fatalf("BishopAttacks(%d, %#016x) = %#016x, want %#016x", sq, occ, got, want)
			}
		}
	}
}
