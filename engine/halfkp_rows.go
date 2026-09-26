package engine

// maxAccRows is how many first-layer rows one accumulator update batches. A
// capture is three (the mover leaves, arrives, the victim leaves), a
// capture-promotion three as well, so four covers every legal move.
const maxAccRows = 4

// accRows sets dst[i] = src[i] + signs[0]*rows[0][i] + ... + signs[k-1]*rows[k-1][i],
// added left to right, which is the order a copy followed by one pass per
// row produces. Signs are +1 or -1, so each product is exact and fusing it
// into the add cannot change a bit either.
//
// One pass per k instead of a copy plus k read-modify-write passes: in
// quiescence nearly every move is a capture, and the three-row case was the
// single hottest loop in the engine.
func accRows(dst, src []float32, rows *[maxAccRows][]float32, signs *[maxAccRows]float32, k int) {
	n := len(dst)
	if haveAccRowsNEON && n > 0 && n%16 == 0 {
		// The vector kernel trusts its lengths, so they are checked here.
		_ = src[n-1]
		for j := 0; j < k; j++ {
			_ = rows[j][n-1]
		}
		accRowsNEON(&dst[0], &src[0], rows, signs, k, n)
		return
	}
	accRowsGo(dst, src, rows, signs, k)
}

// accRowsGo is accRows one unit at a time: what every platform without the
// vector kernel runs, and the reference the kernel is tested against.
func accRowsGo(dst, src []float32, rows *[maxAccRows][]float32, signs *[maxAccRows]float32, k int) {
	n := len(dst)
	src = src[:n]
	switch k {
	case 0:
		copy(dst, src)
	case 1:
		w0, s0 := rows[0][:n], signs[0]
		for i := range dst {
			dst[i] = src[i] + s0*w0[i]
		}
	case 2:
		w0, w1 := rows[0][:n], rows[1][:n]
		s0, s1 := signs[0], signs[1]
		for i := range dst {
			v := src[i] + s0*w0[i]
			dst[i] = v + s1*w1[i]
		}
	case 3:
		w0, w1, w2 := rows[0][:n], rows[1][:n], rows[2][:n]
		s0, s1, s2 := signs[0], signs[1], signs[2]
		for i := range dst {
			v := src[i] + s0*w0[i]
			v += s1 * w1[i]
			dst[i] = v + s2*w2[i]
		}
	default:
		w0, w1, w2, w3 := rows[0][:n], rows[1][:n], rows[2][:n], rows[3][:n]
		s0, s1, s2, s3 := signs[0], signs[1], signs[2], signs[3]
		for i := range dst {
			v := src[i] + s0*w0[i]
			v += s1 * w1[i]
			v += s2 * w2[i]
			dst[i] = v + s3*w3[i]
		}
	}
}
