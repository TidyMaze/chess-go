//go:build !arm64 || purego

package engine

// haveAccRowsNEON is false off arm64: accRows always runs accRowsGo.
const haveAccRowsNEON = false

func accRowsNEON(dst, src *float32, rows *[maxAccRows][]float32, signs *[maxAccRows]float32, k, n int) {
	panic("accRowsNEON: no vector kernel on this platform")
}
