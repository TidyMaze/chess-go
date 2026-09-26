//go:build arm64 && !purego

package engine

// haveAccRowsNEON routes accumulator updates whose width is a multiple of
// 16 to the NEON kernel.
const haveAccRowsNEON = true

// accRowsNEON is accRows sixteen units per iteration: NEON adds four float32
// lanes per instruction and the Go compiler does not vectorise. Each lane is still src,
// then row 0, then row 1..., rounded after every add, and a lane's float32
// add is the scalar one, so the result is the portable kernel's to the bit.
// The rows are applied as FMLA by a vector of their ±1 sign: that product
// is exact, so the fused add rounds once, exactly like a plain add.
//
// n is a positive multiple of 16 and every pointer covers n floats; k is 0
// to maxAccRows.
//
//go:noescape
func accRowsNEON(dst, src *float32, rows *[maxAccRows][]float32, signs *[maxAccRows]float32, k, n int)
