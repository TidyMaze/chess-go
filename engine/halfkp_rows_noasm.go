//go:build !arm64 || purego

package engine

// haveAccRowsNEON is false off arm64: every kernel runs its Go version.
const haveAccRowsNEON = false

func accRowsNEON(dst, src *float32, rows *[maxAccRows][]float32, signs *[maxAccRows]float32, k, n int) {
	panic("accRowsNEON: no vector kernel on this platform")
}

func accPairNEON(dst0, src0, dst1, src1, w0, w1 *float32, rows *[2][maxAccRows]int32, signs *[maxAccRows]float32, k, n int) {
	panic("accPairNEON: no vector kernel on this platform")
}

func accFeatsNEON(dst, src, w *float32, rows *int32, nf, n int) {
	panic("accFeatsNEON: no vector kernel on this platform")
}

func headSumNEON(out float32, w, a *float32, n int) float32 {
	panic("headSumNEON: no vector kernel on this platform")
}
