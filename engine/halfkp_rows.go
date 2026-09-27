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

// accPair is accRows for both perspectives of a node in one call, each row
// named by its index into that perspective's king-slot block of W1 instead
// of by a slice header: row i of w is w[i*n : i*n+n], n = len(dst0).
// dst0 = src0 + the rows rows[0][:k] of w0, then, unless dst1 is nil,
// dst1 = src1 + the rows rows[1][:k] of w1, with the same signs. Per unit
// the order and rounding are accRowsGo's.
//
// Building eight slice headers, bounds-checking every row and calling the
// kernel twice cost more per node than the kernel itself. The vector path
// checks only that each block holds a whole king slot, halfKPPerKing rows,
// and trusts every index to be below that, which diffAcc and rebuild
// guarantee by construction.
func accPair(dst0, src0, dst1, src1, w0, w1 []float32, rows *[2][maxAccRows]int32, signs *[maxAccRows]float32, k int) {
	n := len(dst0)
	if haveAccRowsNEON && n > 0 && n%16 == 0 {
		_, _ = src0[n-1], w0[halfKPPerKing*n-1]
		var d1, s1, b1 *float32
		if dst1 != nil {
			_, _, _ = dst1[n-1], src1[n-1], w1[halfKPPerKing*n-1]
			d1, s1, b1 = &dst1[0], &src1[0], &w1[0]
		}
		accPairNEON(&dst0[0], &src0[0], d1, s1, &w0[0], b1, rows, signs, min(k, maxAccRows), n)
		return
	}
	accPairGo(dst0, src0, dst1, src1, w0, w1, rows, signs, k)
}

// accPairGo is accPair through accRowsGo, with every row bounds-checked:
// what platforms without the vector kernels run.
func accPairGo(dst0, src0, dst1, src1, w0, w1 []float32, rows *[2][maxAccRows]int32, signs *[maxAccRows]float32, k int) {
	n := len(dst0)
	var rs [maxAccRows][]float32
	for p, dst := range [2][]float32{dst0, dst1} {
		if dst == nil {
			continue
		}
		src, w := src0, w0
		if p == 1 {
			src, w = src1, w1
		}
		for j := 0; j < k; j++ {
			o := int(rows[p][j]) * n
			rs[j] = w[o : o+n : o+n]
		}
		accRowsGo(dst[:n], src[:n], &rs, signs, k)
	}
}

// accFeats sets dst = src plus every row of w that rows names (row i is
// w[i*n : i*n+n]), added in rows' order: a full rebuild in one pass over
// dst, instead of a store and reload every maxAccRows rows. The vector
// path trusts the indices the way accPair's does.
func accFeats(dst, src, w []float32, rows []int32) {
	n := len(dst)
	if haveAccRowsNEON && n > 0 && n%16 == 0 {
		_, _ = src[n-1], w[halfKPPerKing*n-1]
		var r *int32
		if len(rows) > 0 {
			r = &rows[0]
		}
		accFeatsNEON(&dst[0], &src[0], &w[0], r, len(rows), n)
		return
	}
	copy(dst, src[:n])
	for _, i := range rows {
		o := int(i) * n
		row := w[o : o+n : o+n]
		for u := range dst {
			dst[u] += row[u]
		}
	}
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
