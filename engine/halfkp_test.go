package engine

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"chess/board"
	"chess/game"
)

// These tests exist because the previous network was measured for a long
// time before anyone asked whether it was computing what it was supposed
// to compute. A network that is wired wrongly still trains, still reports
// a falling loss, and still loses every game, and none of those signals
// distinguishes it from one that is merely undertrained.

func TestHalfKPIndicesAreInRangeAndDistinct(t *testing.T) {
	seen := map[int]string{}
	// One king square per bucket, so distinct features stay distinct.
	for _, kingSq := range []board.Sq{{0, 0}, {1, 0}, {2, 0}, {3, 0}, {0, 7}, {1, 7}, {2, 7}, {3, 7}} {
		for _, pt := range []board.PieceType{board.Pawn, board.Knight, board.Bishop, board.Rook, board.Queen} {
			for _, owner := range []board.Color{board.White, board.Black} {
				for f := int8(0); f < 8; f++ {
					for r := int8(0); r < 8; r++ {
						sq := board.Sq{File: f, Rank: r}
						_ = sq
						idx, ok := halfKPIndex(kingSq, pt, owner, sq, board.White, halfKPKingBuckets)
						if !ok {
							t.Fatalf("piece %v was rejected", pt)
						}
						if idx < 0 || idx >= HalfKPInputs {
							t.Fatalf("index %d out of range [0,%d)", idx, HalfKPInputs)
						}
						key := fmt.Sprintf("king%v %v %v on %v", kingSq, pt, owner, sq)
						if prev, dup := seen[idx]; dup {
							t.Fatalf("index %d collides: %s and %s", idx, prev, key)
						}
						seen[idx] = key
					}
				}
			}
		}
	}
}

func TestHalfKPExcludesKings(t *testing.T) {
	if _, ok := halfKPIndex(board.Sq{4, 0}, board.King, board.White, board.Sq{4, 0}, board.White, halfKPKingBuckets); ok {
		t.Error("kings must not produce a piece feature: the king square is the conditioning variable")
	}
}

// The whole point of HalfKP is that the same piece on the same square is
// a different feature when the king stands elsewhere. Without this the
// feature set is just a piece-square table and cannot express king
// safety, which is what the previous 768-input network could not do.
func TestHalfKPIsConditionedOnTheKing(t *testing.T) {
	// Across buckets: a king on e1 and one on a1 are different situations.
	a, _ := halfKPIndex(board.Sq{4, 0}, board.Knight, board.White, board.Sq{5, 2}, board.White, halfKPKingBuckets)
	b, _ := halfKPIndex(board.Sq{0, 0}, board.Knight, board.White, board.Sq{5, 2}, board.White, halfKPKingBuckets)
	if a == b {
		t.Error("kings in different buckets produced the same feature index")
	}
	// Within a bucket they deliberately share, which is the point: it is
	// what multiplies the data behind each weight.
	c, _ := halfKPIndex(board.Sq{4, 0}, board.Knight, board.White, board.Sq{5, 2}, board.White, halfKPKingBuckets)
	d, _ := halfKPIndex(board.Sq{4, 1}, board.Knight, board.White, board.Sq{5, 2}, board.White, halfKPKingBuckets)
	if c != d {
		t.Error("kings in the same bucket should share a feature index")
	}
}

// Files are mirrored: a king on b1 and one on g1 are the same situation
// reflected, so they share a bucket.
func TestKingBucketsMirrorFiles(t *testing.T) {
	if kingBucket(board.Sq{1, 0}) != kingBucket(board.Sq{6, 0}) {
		t.Error("b1 and g1 should share a king bucket")
	}
	for f := int8(0); f < 8; f++ {
		for r := int8(0); r < 8; r++ {
			b := kingBucket(board.Sq{f, r})
			if b < 0 || b >= halfKPKingBuckets {
				t.Fatalf("king bucket %d out of range for file %d rank %d", b, f, r)
			}
		}
	}
}

// mirror returns the position with colours swapped and ranks flipped,
// which is the same position with the sides exchanged.
func mirror(b *board.Board) board.Board {
	out := board.NewEmpty()
	var buf [32]board.ColoredPiece
	for _, p := range b.AppendAllPieces(buf[:0]) {
		out.Place(board.Sq{File: p.Sq.File, Rank: 7 - p.Sq.Rank},
			board.Piece{Color: p.Color.Other(), Type: p.Type})
	}
	return out
}

// A position seen from White must produce exactly the features that its
// mirror produces seen from Black. If this fails the network has to learn
// every pattern twice, once per colour, which at this data scale it never
// will.
func TestHalfKPMirrorSymmetry(t *testing.T) {
	g := midOpeningPosition()
	for i := 0; i < 12; i++ {
		m, ok := PlayerPick(Player{Depth: 2, UsePST: true}, g)
		if !ok {
			break
		}
		g.ApplyMove(m.From, m.To)
	}
	original := g.Board
	flipped := mirror(&original)

	var a, b []int32
	a = AppendHalfKPFeatures(a, &original, board.White)
	b = AppendHalfKPFeatures(b, &flipped, board.Black)

	set := func(xs []int32) map[int32]int {
		m := map[int32]int{}
		for _, x := range xs {
			m[x]++
		}
		return m
	}
	sa, sb := set(a), set(b)
	if len(sa) != len(sb) {
		t.Fatalf("different feature counts: %d and %d", len(sa), len(sb))
	}
	for k, v := range sa {
		if sb[k] != v {
			t.Errorf("feature %d appears %d times in the position and %d in its mirror", k, v, sb[k])
		}
	}
}

// ---------------------------------------------------------------------
// Numerical gradient check.
//
// This is the test that matters most. Backpropagation written by hand is
// easy to get subtly wrong, and a wrong gradient still reduces the loss
// (slowly, in the wrong direction) which looks like training. Comparing
// against finite differences is the only cheap way to know.

type miniNet struct {
	h  int
	w1 []float32
	b1 []float32
	w2 []float32
	b2 float32
}

func (n *miniNet) forward(own, opp []int32) (out float32, acc, act []float32) {
	h := n.h
	acc = make([]float32, 2*h)
	act = make([]float32, 2*h)
	copy(acc[:h], n.b1)
	copy(acc[h:], n.b1)
	for side, feats := range [2][]int32{own, opp} {
		off := side * h
		for _, f := range feats {
			col := int(f) * h
			for i := 0; i < h; i++ {
				acc[off+i] += n.w1[col+i]
			}
		}
	}
	out = n.b2
	for i := 0; i < 2*h; i++ {
		a := acc[i]
		if a < 0 {
			a = 0
		} else if a > 1 {
			a = 1
		}
		act[i] = a
		out += n.w2[i] * a
	}
	return out, acc, act
}

func TestBackpropMatchesNumericalGradient(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	h := 6
	// A deliberately small feature space so the finite-difference sweep
	// is affordable; the arithmetic is identical to the real one.
	const inputs = 40
	n := &miniNet{h: h,
		w1: make([]float32, inputs*h),
		b1: make([]float32, h),
		w2: make([]float32, 2*h),
	}
	for i := range n.w1 {
		// Spread so activations land inside the clip, where the gradient
		// is non-zero. A saturated unit legitimately has zero gradient and
		// would make this test vacuous.
		n.w1[i] = float32(rng.NormFloat64() * 0.08)
	}
	for i := range n.w2 {
		n.w2[i] = float32(rng.NormFloat64() * 0.3)
	}
	own := []int32{1, 5, 9, 13}
	opp := []int32{2, 6, 10, 21}
	target := float32(0.62)

	loss := func() float32 {
		out, _, _ := n.forward(own, opp)
		d := out - target
		return d * d
	}

	// Analytic gradient, the same arithmetic the trainer uses.
	out, acc, act := n.forward(own, opp)
	dOut := 2 * (out - target)
	gradHidden := make([]float32, 2*h)
	gradW2 := make([]float32, 2*h)
	for i := 0; i < 2*h; i++ {
		gradW2[i] = dOut * act[i]
		if acc[i] > 0 && acc[i] < 1 {
			gradHidden[i] = dOut * n.w2[i]
		}
	}

	const eps = 1e-3
	check := func(name string, i int, get func() *float32, analytic float32) {
		p := get()
		orig := *p
		*p = orig + eps
		up := loss()
		*p = orig - eps
		down := loss()
		*p = orig
		numeric := (up - down) / (2 * eps)
		if math.Abs(float64(numeric-analytic)) > 1e-2*math.Max(1, math.Abs(float64(numeric))) {
			t.Errorf("%s[%d]: analytic %.6f, numerical %.6f", name, i, analytic, numeric)
		}
	}

	for i := 0; i < 2*h; i++ {
		check("w2", i, func() *float32 { return &n.w2[i] }, gradW2[i])
	}
	check("b2", 0, func() *float32 { return &n.b2 }, dOut)

	// First-layer weights: each active feature's column receives the
	// hidden gradient for its own perspective.
	for side, feats := range [2][]int32{own, opp} {
		for _, f := range feats {
			for i := 0; i < h; i++ {
				idx := int(f)*h + i
				check("w1", idx, func() *float32 { return &n.w1[idx] }, gradHidden[side*h+i])
			}
		}
	}
}

// A network that cannot memorise twenty positions has a broken training
// loop, whatever it does on real data. This separates "the optimiser is
// wrong" from "there is not enough data", which are the two explanations
// for a network that does not work and need completely different fixes.
func TestNetworkCanFitASmallSample(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	g := game.New()
	type ex struct {
		own, opp []int32
		target   float64
	}
	var data []ex
	for len(data) < 20 {
		m, ok := PlayerPick(Player{Depth: 2, UsePST: true, Tapered: true}, g)
		if !ok {
			break
		}
		g.ApplyMove(m.From, m.To)
		var own, opp []int32
		own = AppendHalfKPFeatures(own, &g.Board, board.White)
		opp = AppendHalfKPFeatures(opp, &g.Board, board.Black)
		data = append(data, ex{own, opp, rng.Float64()})
	}

	h := 16
	n := &miniNet{h: h,
		w1: make([]float32, HalfKPInputsFor(halfKPKingBuckets)*h),
		b1: make([]float32, h),
		w2: make([]float32, 2*h),
	}
	for i := range n.w1 {
		n.w1[i] = float32(rng.NormFloat64() * 0.01)
	}
	for i := range n.w2 {
		n.w2[i] = float32(rng.NormFloat64() * 0.1)
	}

	meanLoss := func() float64 {
		total := 0.0
		for _, e := range data {
			out, _, _ := n.forward(e.own, e.opp)
			d := float64(out) - e.target
			total += d * d
		}
		return total / float64(len(data))
	}
	before := meanLoss()

	lr := float32(0.05)
	for epoch := 0; epoch < 400; epoch++ {
		for _, e := range data {
			out, acc, act := n.forward(e.own, e.opp)
			dOut := 2 * (out - float32(e.target))
			grad := make([]float32, 2*h)
			for i := 0; i < 2*h; i++ {
				if acc[i] > 0 && acc[i] < 1 {
					grad[i] = dOut * n.w2[i]
				}
				n.w2[i] -= lr * dOut * act[i]
			}
			n.b2 -= lr * dOut
			for side, feats := range [2][]int32{e.own, e.opp} {
				off := side * h
				for i := 0; i < h; i++ {
					n.b1[i] -= lr * grad[off+i]
				}
				for _, f := range feats {
					col := int(f) * h
					for i := 0; i < h; i++ {
						n.w1[col+i] -= lr * grad[off+i]
					}
				}
			}
		}
	}
	after := meanLoss()
	t.Logf("loss over 20 positions: %.5f -> %.5f", before, after)
	if after > before/10 {
		t.Errorf("training reduced the loss only from %.5f to %.5f; the optimiser is not working", before, after)
	}
}

// A network larger than the stack accumulator must still evaluate.
//
// It used to return exactly 0 for every position instead. Nothing failed,
// nothing logged, and the capacity experiment trained a 256-unit network
// to 62% of held-out variance and was about to race it over 3,000 games
// against the champion; it would have scored like a coin flip and the
// conclusion drawn would have been "capacity does not help".
//
// The silent zero is the defect, more than the bound itself: an
// evaluation that cannot run must not quietly answer "equal".
func TestLargeNetworkStillEvaluates(t *testing.T) {
	for _, h := range []int{16, maxHalfKPHidden, maxHalfKPHidden + 1, 256} {
		n := &HalfKPNet{
			H:  h,
			W1: make([]float32, HalfKPInputsFor(halfKPKingBuckets)*h),
			B1: make([]float32, h),
			W2: make([]float32, 2*h),
			B2: 0.25, Scale: 1,
		}
		// Weights that cannot cancel to zero by accident.
		for i := range n.W1 {
			n.W1[i] = 0.01
		}
		for i := range n.B1 {
			n.B1[i] = 0.5
		}
		for i := range n.W2 {
			n.W2[i] = 0.5
		}
		g, err := game.ParseFEN("r1bq1rk1/pp2bppp/2n1pn2/3p4/3P4/2NBPN2/PP3PPP/R1BQ1RK1 w - - 0 9")
		if err != nil {
			t.Fatal(err)
		}
		if got := n.Evaluate(&g.Board); got == 0 {
			t.Errorf("h=%d: evaluated to exactly 0, so the network is not running at all", h)
		}
	}
}
