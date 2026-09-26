package engine

import (
	"math"
	"math/rand"
	"testing"
)

// accRows is the kernel under every incremental accumulator update: each
// unit receives the source, then every changed row in order. float32
// addition is not associative, so summing the rows first, or in another
// order, would move the last bits of evaluations the search prunes on. The
// agreement is to the bit, over widths that are and are not a multiple of
// any unroll or vector width, and magnitudes spread wide enough that a
// reordered sum rounds differently.
func TestAccRowsIsBitIdenticalToSequentialAdds(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	row := func(h int) []float32 {
		r := make([]float32, h)
		for i := range r {
			r[i] = float32(rng.NormFloat64() * math.Pow(10, float64(rng.Intn(7)-3)))
		}
		return r
	}
	for _, h := range []int{1, 3, 4, 7, 8, 15, 16, 17, 31, 32, 48, 64, 100, 128} {
		for k := 0; k <= maxAccRows; k++ {
			for trial := 0; trial < 20; trial++ {
				src := row(h)
				var rows [maxAccRows][]float32
				var signs [maxAccRows]float32
				for j := 0; j < k; j++ {
					rows[j] = row(h)
					signs[j] = float32(1 - 2*rng.Intn(2))
				}
				want := append([]float32(nil), src...)
				for j := 0; j < k; j++ {
					for i := range want {
						want[i] += signs[j] * rows[j][i]
					}
				}
				// accRows may dispatch to a vector kernel; accRowsGo is the
				// portable one every other platform runs. Both are held to it.
				for name, kernel := range map[string]func([]float32, []float32, *[maxAccRows][]float32, *[maxAccRows]float32, int){
					"accRows": accRows, "accRowsGo": accRowsGo,
				} {
					got := make([]float32, h)
					kernel(got, src, &rows, &signs, k)
					for i := range want {
						if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
							t.Fatalf("%s h=%d k=%d unit %d: kernel %v, sequential adds %v", name, h, k, i, got[i], want[i])
						}
					}
				}
			}
		}
	}
}
