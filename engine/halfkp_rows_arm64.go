//go:build arm64 && !purego

package engine

// haveAccRowsNEON routes accumulator updates whose width is a multiple of
// 16 to the NEON kernels.
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

// accPairNEON is accRowsNEON for up to two perspectives in one call, row i
// of a perspective being the n floats at w + i*n: dst0 = src0 + the rows
// rows[0][:k] of w0, then, unless dst1 is nil, dst1 = src1 + the rows
// rows[1][:k] of w1, with the same signs. Same lane order, same rounding.
//
// n is a positive multiple of 16, every dst and src covers n floats, and
// every row lies inside its block.
//
//go:noescape
func accPairNEON(dst0, src0, dst1, src1, w0, w1 *float32, rows *[2][maxAccRows]int32, signs *[maxAccRows]float32, k, n int)

// accFeatsNEON sets dst = src plus the nf rows rows[:nf] of w (row i being
// the n floats at w + i*n), each lane adding them in rows' order with a
// plain FADD, one rounding per add.
//
// n is a positive multiple of 16, dst and src cover n floats, and every
// row lies inside w.
//
//go:noescape
func accFeatsNEON(dst, src, w *float32, rows *int32, nf, n int)

// headSumNEON is headSum over n units, n a positive multiple of 16, with
// w and a covering n floats each. The clip runs four lanes at a time; the
// sum stays one scalar fused multiply-add per unit, in unit order, so the
// result is the loop's to the bit.
//
//go:noescape
func headSumNEON(out float32, w, a *float32, n int) float32
