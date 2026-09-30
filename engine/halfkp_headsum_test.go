package engine

import (
	"math"
	"math/rand"
	"testing"
)

// headSumReference is the output layer's loop as head wrote it before the
// vector kernel: one fused multiply-add per unit, in unit order, each
// rounding once in float32.
func headSumReference(out float32, w, a []float32) float32 {
	for i := range w {
		out += w[i] * clip01(a[i])
	}
	return out
}

// The vector kernel clips sixteen units at once but keeps the chain of
// fused multiply-adds scalar and in order, so it must match the loop to
// the bit on every input, including units below zero, above one, -0 and
// values whose products round.
func TestHeadSumNEONMatchesTheLoopToTheBit(t *testing.T) {
	if !haveAccRowsNEON {
		t.Skip("no vector kernel on this platform")
	}
	rng := rand.New(rand.NewSource(7))
	special := []float32{0, float32(math.Copysign(0, -1)), 1, -1, 1.0000001, 0.9999999, 1e-30, -1e-30, 3, 1e-8}
	for _, n := range []int{16, 32, 64, 128, 512} {
		for trial := 0; trial < 500; trial++ {
			w, a := make([]float32, n), make([]float32, n)
			for i := range w {
				w[i] = float32(rng.NormFloat64()) * 0.3
				a[i] = float32(rng.Float64()*1.6 - 0.3)
				if rng.Intn(8) == 0 {
					a[i] = special[rng.Intn(len(special))]
				}
			}
			out := float32(rng.NormFloat64())
			want := headSumReference(out, w, a)
			got := headSum(out, w, a)
			if math.Float32bits(got) != math.Float32bits(want) {
				t.Fatalf("n=%d trial %d: got %v (%08x), want %v (%08x)",
					n, trial, got, math.Float32bits(got), want, math.Float32bits(want))
			}
		}
	}
}

// Widths that are not a multiple of sixteen take the loop, and so does an
// empty layer.
func TestHeadSumOddWidthsTakeTheLoop(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for _, n := range []int{0, 1, 5, 17, 33} {
		w, a := make([]float32, n), make([]float32, n)
		for i := range w {
			w[i], a[i] = float32(rng.NormFloat64()), float32(rng.Float64()*1.4-0.2)
		}
		if got, want := headSum(0.5, w, a), headSumReference(0.5, w, a); math.Float32bits(got) != math.Float32bits(want) {
			t.Fatalf("n=%d: got %v, want %v", n, got, want)
		}
	}
}
