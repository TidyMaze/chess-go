package engine

import (
	"math"
	"math/rand"
	"testing"
)

// branchyHead is the output layer as first written: a clipped ReLU made of
// two comparisons, then one multiply-add per unit, own perspective first.
func branchyHead(n *HalfKPNet, own, opp []float32) float64 {
	clip := func(v float32) float32 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	out := n.B2
	for i := 0; i < n.H; i++ {
		out += n.W2[i] * clip(own[i])
	}
	for i := 0; i < n.H; i++ {
		out += n.W2[n.H+i] * clip(opp[i])
	}
	return n.toPawns(out)
}

// A branch-free clip must leave every score identical to the bit, zero's sign
// aside; accumulators straddle both corners and include exact zeros and ones.
func TestHeadMatchesTheBranchyClipToTheBit(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	nets := []*HalfKPNet{}
	if champ, err := LoadHalfKPNet("../champion_net.json"); err == nil && champ.H2 == 0 {
		nets = append(nets, champ)
	}
	// A width that is not a multiple of the unrolled step, for the tail.
	odd := &HalfKPNet{H: 37, Scale: 1, B2: 0.25, W2: make([]float32, 74)}
	for i := range odd.W2 {
		odd.W2[i] = float32(rng.NormFloat64())
	}
	nets = append(nets, odd)
	compared := 0
	for _, n := range nets {
		own, opp := make([]float32, n.H), make([]float32, n.H)
		for trial := 0; trial < 2000; trial++ {
			for _, a := range [][]float32{own, opp} {
				for i := range a {
					switch rng.Intn(6) {
					case 0:
						a[i] = 0
					case 1:
						a[i] = 1
					default:
						a[i] = float32(rng.Float64()*3 - 1)
					}
				}
			}
			got, want := n.head(own, opp), branchyHead(n, own, opp)
			if math.Float64bits(got) != math.Float64bits(want) && !(got == 0 && want == 0) {
				t.Fatalf("h=%d trial %d: head %v, branchy clip %v", n.H, trial, got, want)
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatal("nothing compared")
	}
}
