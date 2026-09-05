package engine

import (
	"fmt"
	"testing"
)

// The table is the largest single leaf in the depth-7 profile, and its
// cost is cache behaviour rather than logic: 2^20 entries at 24 bytes is
// 24 MB, far past any cache, so a probe is a trip to main memory. A
// smaller table collides more but may still win on speed.
func BenchmarkTTSize(b *testing.B) {
	for _, bits := range []uint{16, 18, 20, 22} {
		b.Run(fmt.Sprintf("bits=%d", bits), func(b *testing.B) {
			p := fullPlayer(7)
			p.TTBits = bits
			for i := 0; i < b.N; i++ {
				g := midOpeningPosition()
				PlayerPick(p, g)
			}
		})
	}
}
