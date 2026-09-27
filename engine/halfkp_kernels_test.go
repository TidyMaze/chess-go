package engine

import (
	"math"
	"math/bits"
	"math/rand"
	"testing"

	"chess/board"
	"chess/game"
)

// spreadFloats returns n values whose magnitudes span seven decades, so a
// sum taken in another order rounds differently and a reordering kernel
// fails the bit comparison instead of passing by luck.
func spreadFloats(rng *rand.Rand, n int) []float32 {
	v := make([]float32, n)
	for i := range v {
		v[i] = float32(rng.NormFloat64() * math.Pow(10, float64(rng.Intn(7)-3)))
	}
	return v
}

// guarded returns a slice of length h followed by guard units holding a
// marker, so a kernel that writes past its length is caught.
func guarded(h int) (dst, whole []float32) {
	whole = make([]float32, h+17)
	for i := range whole {
		whole[i] = 12345
	}
	return whole[:h:h], whole
}

func checkGuard(t *testing.T, whole []float32, h int, what string) {
	t.Helper()
	for i := h; i < len(whole); i++ {
		if whole[i] != 12345 {
			t.Fatalf("%s: unit %d past the length %d was written", what, i, h)
		}
	}
}

// accPair updates both perspectives from one call, with rows named by
// their index into each perspective's king-slot block. Each perspective
// must be exactly what accRowsGo makes of the same rows as slices: source,
// then row 0, row 1..., rounded after every add, and nothing written past
// the accumulator's length.
func TestAccPairIsAccRowsGoOnEachPerspective(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, h := range []int{1, 7, 16, 17, 32, 37, 48, 64, 100, 128} {
		blocks := [2][]float32{spreadFloats(rng, halfKPPerKing*h), spreadFloats(rng, halfKPPerKing*h)}
		for k := 0; k <= maxAccRows; k++ {
			for _, both := range []bool{false, true} {
				for trial := 0; trial < 25; trial++ {
					src := [2][]float32{spreadFloats(rng, h), spreadFloats(rng, h)}
					var idx [2][maxAccRows]int32
					var signs [maxAccRows]float32
					var rows [2][maxAccRows][]float32
					for j := 0; j < k; j++ {
						for p := 0; p < 2; p++ {
							i := rng.Intn(halfKPPerKing)
							idx[p][j] = int32(i)
							rows[p][j] = blocks[p][i*h : i*h+h]
						}
						signs[j] = float32(1 - 2*rng.Intn(2))
					}
					var got, whole [2][]float32
					got[0], whole[0] = guarded(h)
					w1 := []float32(nil)
					if both {
						got[1], whole[1] = guarded(h)
						w1 = blocks[1]
					}
					accPair(got[0], src[0], got[1], src[1], blocks[0], w1, &idx, &signs, k)
					for p := 0; p < 2; p++ {
						if p == 1 && !both {
							continue
						}
						want := make([]float32, h)
						accRowsGo(want, src[p], &rows[p], &signs, k)
						for i := range want {
							if math.Float32bits(got[p][i]) != math.Float32bits(want[i]) {
								t.Fatalf("h=%d k=%d both=%v perspective %d unit %d: accPair %v, accRowsGo %v",
									h, k, both, p, i, got[p][i], want[i])
							}
						}
						checkGuard(t, whole[p], h, "accPair")
					}
				}
			}
		}
	}
}

// accFeats is a full rebuild in one pass: the source plus every row, in
// the order given, however many rows there are, and nothing written past
// the accumulator's length.
func TestAccFeatsIsSourcePlusEachRowInOrder(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for _, h := range []int{1, 7, 16, 17, 32, 37, 48, 64, 80, 100, 128} {
		w := spreadFloats(rng, halfKPPerKing*h)
		for nf := 0; nf <= 33; nf++ {
			for trial := 0; trial < 6; trial++ {
				src := spreadFloats(rng, h)
				rows := make([]int32, nf)
				want := append([]float32(nil), src...)
				for j := range rows {
					i := rng.Intn(halfKPPerKing)
					rows[j] = int32(i)
					for u := range want {
						want[u] += w[i*h+u]
					}
				}
				got, whole := guarded(h)
				accFeats(got, src, w, rows)
				for u := range want {
					if math.Float32bits(got[u]) != math.Float32bits(want[u]) {
						t.Fatalf("h=%d rows=%d unit %d: accFeats %v, source plus each row %v", h, nf, u, got[u], want[u])
					}
				}
				checkGuard(t, whole, h, "accFeats")
			}
		}
	}
}

// diffAccLoop is diffAcc as first written, visiting all ten non-king
// bitboards whatever changed: the reference for the version that ORs them
// first and walks only the words that differ.
func diffAccLoop(parent, self *halfKPAcc, d *accDiff) bool {
	k := 0
	for c := 0; c < 2; c++ {
		for t := board.Pawn; t < board.King; t++ {
			changed := parent.pieces[c][t] ^ self.pieces[c][t]
			for changed != 0 {
				if k == maxAccRows {
					return false
				}
				i := bits.TrailingZeros64(changed)
				changed &= changed - 1
				d.piece[k] = uint8(c*halfKPPieceKinds/2 + int(t))
				d.sq[k] = uint8(i)
				d.sign[k] = -1
				if self.pieces[c][t]&(1<<uint(i)) != 0 {
					d.sign[k] = 1
				}
				k++
			}
		}
	}
	d.n = k
	return true
}

// Over snapshot pairs that differ by nothing (a null move), by the two to
// four bits a move makes, by more than one update batches, and in the king
// bitboards the diff must ignore, diffAcc agrees with the loop: the same
// verdict and, when it batches, the same entries in the same order.
func TestDiffAccMatchesTheWordByWordLoop(t *testing.T) {
	rng := rand.New(rand.NewSource(19))
	batched := 0
	for trial := 0; trial < 200000; trial++ {
		var parent, self halfKPAcc
		for c := 0; c < 2; c++ {
			for t := 0; t < 6; t++ {
				parent.pieces[c][t] = rng.Uint64() & rng.Uint64() & rng.Uint64()
			}
		}
		self.pieces = parent.pieces
		for f := rng.Intn(7); f > 0; f-- {
			self.pieces[rng.Intn(2)][rng.Intn(6)] ^= 1 << rng.Intn(64)
		}
		var got, want accDiff
		okGot, okWant := diffAcc(&parent, &self, &got), diffAccLoop(&parent, &self, &want)
		if okGot != okWant {
			t.Fatalf("trial %d: diffAcc batched=%v, loop %v", trial, okGot, okWant)
		}
		if !okWant {
			continue
		}
		batched++
		if got.n != want.n {
			t.Fatalf("trial %d: diffAcc %d entries, loop %d", trial, got.n, want.n)
		}
		for j := 0; j < want.n; j++ {
			if got.piece[j] != want.piece[j] || got.sq[j] != want.sq[j] || got.sign[j] != want.sign[j] {
				t.Fatalf("trial %d entry %d: diffAcc (%d,%d,%v), loop (%d,%d,%v)", trial, j,
					got.piece[j], got.sq[j], got.sign[j], want.piece[j], want.sq[j], want.sign[j])
			}
			// The rows are halfKPIndex's, less the king slot's block.
			owner, pt := board.Color(want.piece[j]/5), board.PieceType(want.piece[j]%5)
			sq := board.Sq{File: int8(want.sq[j] % 8), Rank: int8(want.sq[j] / 8)}
			for p, persp := range [2]board.Color{board.White, board.Black} {
				king := board.Sq{File: int8(rng.Intn(8)), Rank: int8(rng.Intn(8))}
				f, _ := halfKPIndex(king, pt, owner, sq, persp, halfKPKingBuckets)
				row := f - perspectiveKingSlot(king, persp, halfKPKingBuckets)*halfKPPerKing
				if int(got.row[p][j]) != row {
					t.Fatalf("trial %d entry %d perspective %d: diffAcc row %d, halfKPIndex %d", trial, j, p, got.row[p][j], row)
				}
			}
		}
	}
	if batched == 0 {
		t.Fatal("no batched diff compared")
	}
}

// addRowsRef is the full rebuild as it was before accFeats: the bias
// plus the feature rows, four slices at a time through accRows.
func addRowsRef(n *HalfKPNet, a, src []float32, feats []int32) {
	h := n.H
	a, src = a[:h:h], src[:h:h]
	plus := [maxAccRows]float32{1, 1, 1, 1}
	var rows [maxAccRows][]float32
	for {
		k := min(len(feats), maxAccRows)
		for j, f := range feats[:k] {
			col := int(f) * h
			rows[j] = n.W1[col : col+h : col+h]
		}
		accRows(a, src, &rows, &plus, k)
		feats, src = feats[k:], a
		if len(feats) == 0 {
			return
		}
	}
}

// refreshRef is refresh as it was before the two perspectives were fused
// into one kernel call: per perspective, rows batched as slices through
// applyDiff, copy then applyDelta when the diff does not batch, and a full
// rebuild from the feature list otherwise.
func refreshRef(n *HalfKPNet, b *board.Board, self, parent *halfKPAcc, st *halfKPAccStats) {
	self.valid = false
	if n == nil || n.H == 0 || n.H > maxHalfKPHidden || len(n.W1) != HalfKPInputsFor(n.buckets())*n.H {
		return
	}
	h := n.H
	buckets := n.buckets()
	for c := 0; c < 2; c++ {
		for t := board.Pawn; t <= board.King; t++ {
			self.pieces[c][t] = b.PieceBitboard(board.Color(c), t)
		}
	}
	self.kings = [2]board.Sq{b.KingSquare(board.White), b.KingSquare(board.Black)}
	var d accDiff
	diffed, batched := false, false
	for side, persp := range [2]board.Color{board.White, board.Black} {
		a := self.acc[side][:h]
		canInc := false
		var slot int
		if parent != nil && parent.valid && parent.kings[side] == self.kings[side] {
			slot = int(parent.kingSlots[side])
			self.kingSlots[side] = parent.kingSlots[side]
			canInc = true
		} else {
			slot = perspectiveKingSlot(self.kings[side], persp, buckets)
			self.kingSlots[side] = int8(slot)
			canInc = parent != nil && parent.valid && int(parent.kingSlots[side]) == slot
		}
		if canInc {
			if !diffed {
				batched, diffed = diffAccLoop(parent, self, &d), true
			}
			if batched {
				n.applyDiff(a, parent.acc[side][:h], &d, persp, slot)
			} else {
				copy(a, parent.acc[side][:h])
				n.applyDelta(a, parent, self, persp, buckets)
			}
			if st != nil {
				st.incremental++
			}
			continue
		}
		var buf [32]int32
		addRowsRef(n, a, n.B1, AppendHalfKPFeaturesN(buf[:0], b, persp, buckets))
		if st != nil {
			st.full++
		}
	}
	self.valid = true
}

// refresh must leave every field refreshRef leaves, to the bit, whatever
// the parent: the previous ply, none, an invalid one, one two plies back
// (more changes than one update batches, or a king that moved and came
// back), or the same position (a null move). Networks of the champion's
// shape, of widths the vector kernels do and do not take, and of every
// king granularity.
func TestRefreshMatchesThePerPerspectiveReference(t *testing.T) {
	nets := []*HalfKPNet{
		randomHalfKP(64, 0, 8, 1),
		randomHalfKP(48, 0, 32, 2),
		randomHalfKP(37, 0, 8, 3),
		randomHalfKP(16, 0, 64, 4),
		randomHalfKP(128, 0, 8, 5),
	}
	if champ, err := LoadHalfKPNet("../champion_net.json"); err == nil {
		nets = append(nets, champ)
	}
	rng := rand.New(rand.NewSource(29))
	for _, n := range nets {
		h := n.H
		var st, stRef halfKPAccStats
		for _, fen := range correctnessPositions {
			g, err := game.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			var hist []halfKPAcc
			color := g.Turn
			for ply := 0; ply < 60; ply++ {
				var parent *halfKPAcc
				var invalid halfKPAcc
				switch r := rng.Intn(10); {
				case len(hist) == 0 || r == 0:
				case r == 1:
					invalid = hist[len(hist)-1]
					invalid.valid = false
					parent = &invalid
				case r == 2 && len(hist) >= 2:
					parent = &hist[len(hist)-2]
				default:
					parent = &hist[len(hist)-1]
				}
				var got, want halfKPAcc
				n.refresh(&g.Board, &got, parent, &st)
				refreshRef(n, &g.Board, &want, parent, &stRef)
				if got.valid != want.valid || got.kings != want.kings || got.kingSlots != want.kingSlots || got.pieces != want.pieces {
					t.Fatalf("h=%d %s ply %d: snapshot differs: refresh %+v %v %v, reference %+v %v %v", h, fen, ply,
						got.valid, got.kings, got.kingSlots, want.valid, want.kings, want.kingSlots)
				}
				for side := 0; side < 2; side++ {
					for i := 0; i < h; i++ {
						if math.Float32bits(got.acc[side][i]) != math.Float32bits(want.acc[side][i]) {
							t.Fatalf("h=%d %s ply %d side %d unit %d: refresh %v, reference %v", h, fen, ply, side, i,
								got.acc[side][i], want.acc[side][i])
						}
					}
				}
				if st != stRef {
					t.Fatalf("h=%d %s ply %d: stats %+v, reference %+v", h, fen, ply, st, stRef)
				}
				hist = append(hist, got)
				if rng.Intn(8) == 0 {
					continue // same position again next ply: a null move
				}
				legal := g.AllLegalMoves(color)
				if len(legal) == 0 {
					break
				}
				makeSearchMove(g, legal[rng.Intn(len(legal))])
				color = color.Other()
			}
		}
		if st.incremental == 0 || st.full == 0 {
			t.Fatalf("h=%d: %+v, want both incremental and full refreshes compared", h, st)
		}
	}
}

// randomLines returns the boards of random lines from the correctness
// positions, each board one move after the one before it except where a
// new line starts, for refresh benchmarks that should see the captures,
// king moves and promotions a search sees.
func randomLines(b *testing.B) (net *HalfKPNet, boards []board.Board, starts []bool) {
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		b.Skip("no champion network here:", err)
	}
	rng := rand.New(rand.NewSource(31))
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			b.Fatal(err)
		}
		color := g.Turn
		for ply := 0; ply < 40; ply++ {
			boards = append(boards, g.Board)
			starts = append(starts, ply == 0)
			legal := g.AllLegalMoves(color)
			if len(legal) == 0 {
				break
			}
			makeSearchMove(g, legal[rng.Intn(len(legal))])
			color = color.Other()
		}
	}
	return net, boards, starts
}

func benchmarkRefreshLine(b *testing.B, refresh func(*HalfKPNet, *board.Board, *halfKPAcc, *halfKPAcc, *halfKPAccStats)) {
	net, boards, starts := randomLines(b)
	var accs [2]halfKPAcc
	var st halfKPAccStats
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := i % len(boards)
		self, parent := &accs[i&1], &accs[(i+1)&1]
		if starts[j] {
			parent = nil
		}
		refresh(net, &boards[j], self, parent, &st)
	}
}

// BenchmarkRefreshLine and BenchmarkRefreshRefLine time one accumulator
// update per board along the same lines: the fused kernels against the
// per-perspective path they replaced.
func BenchmarkRefreshLine(b *testing.B) {
	benchmarkRefreshLine(b, (*HalfKPNet).refresh)
}

func BenchmarkRefreshRefLine(b *testing.B) {
	benchmarkRefreshLine(b, refreshRef)
}
