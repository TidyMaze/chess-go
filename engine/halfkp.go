package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"os"
	"sync"

	"chess/board"
)

// Sq is aliased so the bucket helper reads cleanly.
type Sq = board.Sq

// HalfKP: the feature set Stockfish's NNUE actually uses.
//
// The previous network had 768 inputs, one per (piece, colour, square).
// That cannot express "this knight is dangerous because their king is on
// g8", because the king's square is not part of any feature. It is close
// to what piece-square tables already compute, which is why it never beat
// them.
//
// HalfKP conditions every piece feature on the king's square. One feature
// is a triple (our king's square, a piece, that piece's square), giving
// 64 x 10 x 64 = 40,960 inputs. Kings are excluded from the piece part
// because their square is already the conditioning variable.
//
// The network is evaluated from both sides: one accumulator indexed by
// White's king and one by Black's, sharing the same first-layer weights,
// concatenated before the output layer. That is what lets one set of
// weights describe "good for the side to move" symmetrically.
// maxHalfKPHidden is how wide a network can be before the accumulator
// stops fitting comfortably on the stack. Wider networks still work; they
// borrow from accPool instead.
const maxHalfKPHidden = 128

// accPool holds accumulators for networks too wide for the stack path.
var accPool = sync.Pool{New: func() any {
	b := make([]float32, 2*maxHalfKPHidden*4)
	return &b
}}

// King buckets, as modern NNUE feature sets use.
//
// Indexing on all 64 king squares gives 40,960 inputs, and at the data
// scale available here that is too many: adjacent king squares learn
// completely independent weights, so the evaluation is jagged. Measured
// on a 40,960-input network, the score moved 1.25 pawns after a single
// quiet move against the hand-written evaluation's 0.35, and the search
// prunes on pawn thresholds so that jumpiness is fatal.
//
// Buckets group king squares that mean roughly the same thing, so every
// position in a bucket contributes to the same weights. Eight buckets
// (files halved and mirrored, ranks in quarters) cut the input count
// eightfold and multiply the data behind each weight by the same factor.
const halfKPKingBuckets = 8

// halfKPKingSquares is the finer alternative: 32 canonical king squares
// rather than 8 buckets, files mirrored so the king is always on the
// queenside half. This is what HalfKA-style feature sets use. It is four
// times as many inputs, so it needs four times the data behind each
// weight to be worth having, which is why it is a runtime choice and not
// a constant.
const halfKPKingSquares = 32

const (
	halfKPPieceKinds = 10 // 5 piece types x 2 colours, kings excluded
	halfKPPerKing    = halfKPPieceKinds * 64
	HalfKPInputs     = halfKPKingBuckets * halfKPPerKing // 5,120
)

// kingBucket groups king squares. Files are mirrored about the centre,
// because a king on b1 and one on g1 are the same situation reflected,
// and ranks are grouped in pairs: the distinctions that matter are
// "castled short or long" and "back rank or advanced", not the exact
// square.
func kingBucket(s Sq) int {
	file := int(s.File)
	if file > 3 {
		file = 7 - file
	}
	return (int(s.Rank)/4)*4 + file
}

// kingCanonicalSquare is the 32-way version: every rank kept, files
// mirrored about the centre.
func kingCanonicalSquare(s Sq) int {
	file := int(s.File)
	if file > 3 {
		file = 7 - file
	}
	return int(s.Rank)*4 + file
}

// kingSlot picks between them. buckets is 8 or 32; anything else is
// treated as 8, which is what every network written before this existed
// was trained with.
func kingSlot(s Sq, buckets int) int {
	switch buckets {
	case halfKPKingSquares:
		return kingCanonicalSquare(s)
	case halfKPRawKingSquares:
		return int(s.Rank)*8 + int(s.File)
	}
	return kingBucket(s)
}

// halfKPRawKingSquares keys on the exact king square, nothing mirrored, so a
// pool generated this way can be re-bucketed into any coarser scheme.
const halfKPRawKingSquares = 64

// halfKPPieceIndex maps a piece to its slot, relative to the perspective
// being computed: 0-4 are the perspective side's pieces, 5-9 the enemy's.
// Returns false for kings, which are the conditioning variable.
func halfKPPieceIndex(pt board.PieceType, owner, perspective board.Color) (int, bool) {
	if pt == board.King {
		return 0, false
	}
	base := 0
	if owner != perspective {
		base = 5
	}
	// Pawn..Queen are 0..4 in whatever order the board package defines,
	// which does not matter as long as it is consistent.
	idx := int(pt)
	if idx > int(board.King) {
		idx--
	}
	return base + idx, true
}

// halfKPIndex is the input neuron for one piece, seen from one side.
//
// Squares are mirrored vertically for Black so that "my side of the
// board" means the same thing to both perspectives. Without that the
// network has to learn every pattern twice.
func halfKPIndex(kingSq board.Sq, pt board.PieceType, owner board.Color, sq board.Sq, perspective board.Color, buckets int) (int, bool) {
	pi, ok := halfKPPieceIndex(pt, owner, perspective)
	if !ok {
		return 0, false
	}
	ks, ps := kingSq, sq
	if perspective == board.Black {
		ks = board.Sq{File: ks.File, Rank: 7 - ks.Rank}
		ps = board.Sq{File: ps.File, Rank: 7 - ps.Rank}
	}
	k := kingSlot(ks, buckets)
	s := int(ps.Rank)*8 + int(ps.File)
	return k*halfKPPerKing + pi*64 + s, true
}

// AppendHalfKPFeatures writes the active input indices for one
// perspective. At most 30 of 40,960 are ever set, which is what makes
// the first layer affordable: it costs one column addition per piece.
func AppendHalfKPFeatures(dst []int32, b *board.Board, perspective board.Color) []int32 {
	return AppendHalfKPFeaturesN(dst, b, perspective, FeatureKingBuckets)
}

// AppendHalfKPFeaturesN is the same with an explicit king granularity, so
// a network trained on 32 canonical king squares and one trained on 8
// buckets can coexist rather than one silently mis-indexing the other.
func AppendHalfKPFeaturesN(dst []int32, b *board.Board, perspective board.Color, buckets int) []int32 {
	king := b.KingSquare(perspective)
	var buf [32]board.ColoredPiece
	for _, p := range b.AppendAllPieces(buf[:0]) {
		if i, ok := halfKPIndex(king, p.Type, p.Color, p.Sq, perspective, buckets); ok {
			dst = append(dst, int32(i))
		}
	}
	return dst
}

// FeatureKingBuckets is the granularity used when generating training
// data and when a network does not say which it wants. Set by the
// training command; 8 is what every network before today used.
var FeatureKingBuckets = halfKPKingBuckets

// HalfKPInputsFor is the input count for a given granularity.
func HalfKPInputsFor(buckets int) int { return buckets * halfKPPerKing }

// HalfKPNet is the trained network: a shared first layer applied to both
// perspectives, concatenated, then either one hidden-to-output layer or,
// when H2 is set, a second hidden layer before it.
//
//	40960 -> H  (shared, applied twice)
//	2H    -> 1        without a second layer
//	2H    -> H2 -> 1  with one
type HalfKPNet struct {
	H  int       `json:"h"`
	W1 []float32 `json:"w1"` // HalfKPInputs * H
	B1 []float32 `json:"b1"` // H
	W2 []float32 `json:"w2"` // 2H, own perspective first
	B2 float32   `json:"b2"`
	// Scale converts the output into pawns.
	Scale float32 `json:"scale"`
	// Sigmoid marks a network whose output is a win probability rather
	// than a score, because it was trained against a probability target.
	// Evaluate then inverts the sigmoid to get pawns back.
	//
	// Without this the engine reads a probability as a score, and the
	// consequence is not a scaling error but a sign error: probabilities
	// are never negative, so every lost position evaluates as slightly
	// good. Measured on a network explaining 82% of held-out variance,
	// "Black is a rook up" came out at +0.245.
	Sigmoid bool    `json:"sigmoid"`
	K       float64 `json:"k"`
	// Buckets is the king granularity this network was trained with, 8 or
	// 32. Zero means 8, which is what every network written before this
	// field existed used.
	Buckets int `json:"buckets,omitempty"`
	// H2 is the width of an optional second hidden layer between the
	// concatenated accumulators and the output, clipped like the first.
	// Zero, and the three fields absent from the file, is the original
	// shape, which is what every network trained before this existed has.
	//
	// One clipped-linear layer can only add up an opinion per piece, so
	// width, king buckets and averaging all landed at the same 91% of the
	// teacher explained. Two layers can express that a knight on e5 is
	// worth more because their bishop is gone, which is the shape tactics
	// take, and is what real NNUE does (256x2 -> 32 -> 32 -> 1).
	H2 int `json:"h2,omitempty"`
	// WH2 is 2H x H2, input-major like W1: the weights leaving
	// accumulator unit i occupy wh2[i*H2 : i*H2+H2]. Feature-major there
	// and input-major here for the same reason, the forward pass walks
	// one input at a time and wants its weights contiguous.
	WH2 []float32 `json:"wh2,omitempty"`
	BH2 []float32 `json:"bh2,omitempty"` // H2
}

// buckets returns the granularity, defaulting to 8 for older files.
func (n *HalfKPNet) buckets() int {
	if n == nil || n.Buckets == 0 {
		return halfKPKingBuckets
	}
	return n.Buckets
}

// probabilityToPawns is the inverse of the sigmoid used in training.
//
// Clamped away from 0 and 1 because the inverse diverges there, and
// capped at 12 pawns because beyond that the position is decided and the
// search only needs to know the sign.
func probabilityToPawns(p, k float64) float64 {
	const eps = 1e-4
	if p < eps {
		p = eps
	} else if p > 1-eps {
		p = 1 - eps
	}
	v := math.Log(p/(1-p)) / k
	if v > 12 {
		return 12
	}
	if v < -12 {
		return -12
	}
	return v
}

// Evaluate returns the score in pawns from White's point of view.
func (n *HalfKPNet) Evaluate(b *board.Board) float64 {
	if n == nil || n.H == 0 || len(n.W1) != HalfKPInputsFor(n.buckets())*n.H {
		return 0
	}
	h := n.H
	// Stack array, not an allocation. This runs once per evaluated
	// position, which a search does millions of times per move, so an
	// allocation here would dominate everything else the engine does.
	var accArr [2 * maxHalfKPHidden]float32
	var acc []float32
	if h <= maxHalfKPHidden {
		acc = accArr[: 2*h : 2*h]
	} else {
		// A network wider than the stack bound used to return exactly 0
		// here, for every position. Nothing failed and nothing logged: a
		// 256-unit network trained to 62% of held-out variance was about
		// to be raced over 3,000 games while evaluating every position as
		// equal, and the conclusion would have been that capacity does not
		// help.
		//
		// An evaluation that cannot run must not quietly answer "equal".
		// Wide networks now borrow a buffer instead, which costs a pool
		// round trip per evaluation and is the price of being able to try
		// them at all.
		buf := accPool.Get().(*[]float32)
		if cap(*buf) < 2*h {
			*buf = make([]float32, 2*h)
		}
		acc = (*buf)[:2*h]
		defer accPool.Put(buf)
	}
	copy(acc[:h], n.B1)
	copy(acc[h:], n.B1)

	var buf [32]int32
	for side, persp := range [2]board.Color{board.White, board.Black} {
		off := side * h
		for _, f := range AppendHalfKPFeaturesN(buf[:0], b, persp, n.buckets()) {
			col := int(f) * h
			w := n.W1[col : col+h : col+h]
			a := acc[off : off+h]
			// Unrolled by eight. Go does not vectorise this loop and it
			// is the hottest in the engine: one add per hidden unit per
			// active feature per evaluation. Each a[i] still receives its
			// features in the same order, so the result is bit-identical
			// to the plain loop and the node count of a search does not
			// move by one.
			i := 0
			for ; i+8 <= h; i += 8 {
				a8 := a[i : i+8 : i+8]
				w8 := w[i : i+8 : i+8]
				a8[0] += w8[0]
				a8[1] += w8[1]
				a8[2] += w8[2]
				a8[3] += w8[3]
				a8[4] += w8[4]
				a8[5] += w8[5]
				a8[6] += w8[6]
				a8[7] += w8[7]
			}
			for ; i < h; i++ {
				a[i] += w[i]
			}
		}
	}

	return n.head(acc[:h:h], acc[h:2*h:2*h])
}

// clip01 is the clipped ReLU both layers use. min and max compile to FMAX and
// FMIN: two comparisons were two branches per unit that no predictor guesses.
func clip01(v float32) float32 {
	return min(max(v, 0), 1)
}

// toPawns turns the last layer's number into a score the search can use.
func (n *HalfKPNet) toPawns(out float32) float64 {
	if n.Sigmoid {
		k := n.K
		if k == 0 {
			k = 0.30
		}
		return probabilityToPawns(float64(out), k)
	}
	return float64(out * n.Scale)
}

// headSum is out plus w[i] * clip01(a[i]) for every unit, in unit order,
// each a fused multiply-add rounded once in float32. Widths that are a
// multiple of 16 take the NEON kernel: the loop spent more instructions
// on the clip and its constants than on the sum.
func headSum(out float32, w, a []float32) float32 {
	h := len(w)
	a = a[:h:h]
	if haveAccRowsNEON && h > 0 && h%16 == 0 {
		return headSumNEON(out, &w[0], &a[0], h)
	}
	i := 0
	for ; i+4 <= h; i += 4 {
		w4, a4 := w[i:i+4:i+4], a[i:i+4:i+4]
		out += w4[0] * clip01(a4[0])
		out += w4[1] * clip01(a4[1])
		out += w4[2] * clip01(a4[2])
		out += w4[3] * clip01(a4[3])
	}
	for ; i < h; i++ {
		out += w[i] * clip01(a[i])
	}
	return out
}

// head runs everything above the accumulator:the clipped ReLU on both
// perspectives, the optional second hidden layer, then the linear output.
//
// own and opp are the two raw accumulator halves, White's perspective
// first. Nothing here allocates: the second layer's activations are a
// stack array bounded by maxHalfKPHidden, which is also what a network is
// allowed to declare as H2.
func (n *HalfKPNet) head(own, opp []float32) float64 {
	h := n.H
	if n.H2 == 0 {
		// Own perspective in full, then the other, in that order. float32
		// addition is not associative, so any other order would move the
		// last digits of every network trained before this layer existed,
		// and every calibration made against them.
		out := n.B2
		wOwn := n.W2[:h:h]
		wOpp := n.W2[h : 2*h : 2*h]
		own = own[:h:h]
		opp = opp[:h:h]

		out = headSum(out, wOwn, own)
		out = headSum(out, wOpp, opp)
		return n.toPawns(out)
	}
	var midArr [maxHalfKPHidden]float32
	mid := midArr[:n.H2:n.H2]
	copy(mid, n.BH2)
	for i := 0; i < h; i++ {
		n.spread(mid, i, clip01(own[i]))
	}
	for i := 0; i < h; i++ {
		n.spread(mid, h+i, clip01(opp[i]))
	}
	out := n.B2
	for j := 0; j < n.H2; j++ {
		out += n.W2[j] * clip01(mid[j])
	}
	return n.toPawns(out)
}

// spread adds one clipped accumulator unit's contribution to every unit
// of the second hidden layer. WH2 is input-major, so the weights it needs
// are one contiguous run.
func (n *HalfKPNet) spread(mid []float32, i int, v float32) {
	w := n.WH2[i*n.H2 : i*n.H2+n.H2 : i*n.H2+n.H2]
	for j := range mid {
		mid[j] += w[j] * v
	}
}

func LoadHalfKPNet(path string) (*HalfKPNet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var n HalfKPNet
	if err := json.Unmarshal(data, &n); err != nil {
		return nil, err
	}
	if n.Scale == 0 {
		n.Scale = 1
	}
	// Validated at load, not at evaluation. A network whose weight count
	// does not match its declared shape used to load happily and then
	// evaluate every position as exactly 0, which is the failure mode that
	// cost a whole capacity experiment: nothing errors, nothing logs, and
	// the engine plays as though every position were equal.
	if want := HalfKPInputsFor(n.buckets()) * n.H; n.H == 0 || len(n.W1) != want {
		return nil, fmt.Errorf("halfkp %s: %d first-layer weights for %d hidden units "+
			"at %d king buckets, want %d", path, len(n.W1), n.H, n.buckets(), want)
	}
	if len(n.B1) != n.H {
		return nil, fmt.Errorf("halfkp %s: %d first-layer biases for %d hidden units",
			path, len(n.B1), n.H)
	}
	// The output layer reads 2H units without a second hidden layer and H2
	// with one, so which length is right depends on H2.
	if n.H2 < 0 || n.H2 > maxHalfKPHidden {
		return nil, fmt.Errorf("halfkp %s: second hidden layer of %d units, "+
			"the engine evaluates up to %d", path, n.H2, maxHalfKPHidden)
	}
	last := 2 * n.H
	if n.H2 > 0 {
		last = n.H2
		if len(n.WH2) != 2*n.H*n.H2 || len(n.BH2) != n.H2 {
			return nil, fmt.Errorf("halfkp %s: second layer has %d weights and %d biases, "+
				"want %d and %d for %d units over %d accumulator inputs",
				path, len(n.WH2), len(n.BH2), 2*n.H*n.H2, n.H2, n.H2, 2*n.H)
		}
	}
	if len(n.W2) != last {
		return nil, fmt.Errorf("halfkp %s: %d output weights, want %d",
			path, len(n.W2), last)
	}
	return &n, nil
}

func (n *HalfKPNet) Save(path string) error {
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// halfKPAcc is one ply's accumulator state, kept by the search so a child
// can be derived from its parent instead of recomputed.
//
// This is the trick that makes NNUE affordable. A move changes two to
// four of the thirty-odd active features per perspective; recomputing
// every row per node was 21% of the engine's CPU.
type halfKPAcc struct {
	valid bool
	// The position this accumulator describes, as the twelve piece
	// bitboards and the two king squares. A child derives its feature
	// changes from the XOR against its parent's snapshot: the set bits are
	// exactly the pieces that appeared or vanished, two to four per move.
	pieces    [2][6]uint64
	kings     [2]board.Sq
	kingSlots [2]int8
	acc       [2][maxHalfKPHidden]float32
}

// halfKPAccStats counts how each perspective's accumulator was obtained,
// so two per node.
type halfKPAccStats struct{ incremental, full int }

// accSlots is one per search ply plus room for quiescence below it.
const accSlots = maxSearchPly + 48

// refresh makes self valid for b. Each perspective is derived from the
// parent by applying only the rows whose feature changed, when the parent
// is valid and that perspective's king slot is unchanged; otherwise it is
// rebuilt in full from the bias.
//
// The change set comes from XORing parent and child piece bitboards, not
// from re-extracting the feature list: that extraction, plus the sort the
// merge-diff needed, was a fifth of the engine's CPU at both 10 ms and
// 1 s a move. A null move leaves every bitboard equal and costs one copy.
// Networks wider than the stack bound leave self invalid and evaluate the
// slow way.
func (n *HalfKPNet) refresh(b *board.Board, self, parent *halfKPAcc, st *halfKPAccStats) {
	self.valid = false
	if n == nil || n.H == 0 || n.H > maxHalfKPHidden || len(n.W1) != HalfKPInputsFor(n.buckets())*n.H {
		return
	}
	h := n.H
	buckets := n.buckets()
	snapshotPieces(&self.pieces, b)
	self.kings = [2]board.Sq{b.KingSquare(board.White), b.KingSquare(board.Black)}
	fromParent := parent != nil && parent.valid
	var inc [2]bool
	for side, persp := range [2]board.Color{board.White, board.Black} {
		if fromParent && parent.kings[side] == self.kings[side] {
			self.kingSlots[side] = parent.kingSlots[side]
			inc[side] = true
			continue
		}
		slot := perspectiveKingSlot(self.kings[side], persp, buckets)
		self.kingSlots[side] = int8(slot)
		inc[side] = fromParent && int(parent.kingSlots[side]) == slot
	}
	// The changed pieces are the same for both perspectives, only their
	// feature indices differ, so the diff is taken at most once per node,
	// and when both perspectives take it, one kernel call applies it to both.
	var d accDiff
	batched := (inc[0] || inc[1]) && diffAcc(parent, self, &d)
	if batched && inc[0] && inc[1] {
		w0, w1 := n.slotRows(int(self.kingSlots[0])), n.slotRows(int(self.kingSlots[1]))
		if haveAccRowsNEON && h%16 == 0 {
			// accPair's vector path, called directly on the hottest line:
			// the accumulators are arrays of maxHalfKPHidden >= h floats and
			// slotRows has bounded both blocks, which is all it checks.
			accPairNEON(&self.acc[0][0], &parent.acc[0][0], &self.acc[1][0], &parent.acc[1][0],
				&w0[0], &w1[0], &d.row, &d.sign, d.n, h)
		} else {
			accPair(self.acc[0][:h], parent.acc[0][:h], self.acc[1][:h], parent.acc[1][:h], w0, w1, &d.row, &d.sign, d.n)
		}
	} else {
		for side, persp := range [2]board.Color{board.White, board.Black} {
			a := self.acc[side][:h]
			switch {
			case inc[side] && batched:
				rows := [2][maxAccRows]int32{d.row[side]}
				accPair(a, parent.acc[side][:h], nil, nil, n.slotRows(int(self.kingSlots[side])), nil, &rows, &d.sign, d.n)
			case inc[side]:
				copy(a, parent.acc[side][:h])
				n.applyDelta(a, parent, self, persp, buckets)
			default:
				n.rebuild(a, b, persp, int(self.kingSlots[side]))
			}
		}
	}
	if st != nil {
		for _, i := range inc {
			if i {
				st.incremental++
			} else {
				st.full++
			}
		}
	}
	self.valid = true
}

// snapshotPieces copies the twelve piece bitboards, written out rather
// than looped so it compiles to twelve loads and stores.
func snapshotPieces(dst *[2][6]uint64, b *board.Board) {
	w, k := board.White, board.Black
	dst[0][board.Pawn] = b.PieceBitboard(w, board.Pawn)
	dst[0][board.Knight] = b.PieceBitboard(w, board.Knight)
	dst[0][board.Bishop] = b.PieceBitboard(w, board.Bishop)
	dst[0][board.Rook] = b.PieceBitboard(w, board.Rook)
	dst[0][board.Queen] = b.PieceBitboard(w, board.Queen)
	dst[0][board.King] = b.PieceBitboard(w, board.King)
	dst[1][board.Pawn] = b.PieceBitboard(k, board.Pawn)
	dst[1][board.Knight] = b.PieceBitboard(k, board.Knight)
	dst[1][board.Bishop] = b.PieceBitboard(k, board.Bishop)
	dst[1][board.Rook] = b.PieceBitboard(k, board.Rook)
	dst[1][board.Queen] = b.PieceBitboard(k, board.Queen)
	dst[1][board.King] = b.PieceBitboard(k, board.King)
}

// slotRows is the block of W1 a king slot's features index: halfKPPerKing
// rows of H floats. The slice bounds are the check that makes every row
// index below halfKPPerKing safe for the vector kernels, which trust them.
func (n *HalfKPNet) slotRows(slot int) []float32 {
	size := halfKPPerKing * n.H
	return n.W1[slot*size : slot*size+size]
}

// rebuild sets a to the bias plus every feature's row for perspective
// persp, whose king sits in slot, in AppendHalfKPFeaturesN's order (the
// board's occupied-list order): halfKPIndex unrolled the way diffAcc
// unrolls it, then one pass of accFeats.
func (n *HalfKPNet) rebuild(a []float32, b *board.Board, persp board.Color, slot int) {
	flip := 0
	if persp == board.Black {
		flip = 56
	}
	var pieces [32]board.ColoredPiece
	var buf [32]int32
	rows := buf[:0]
	for _, p := range b.AppendAllPieces(pieces[:0]) {
		if p.Type > board.Queen {
			continue // the king: the conditioning variable, not a feature
		}
		pi := int(p.Type)
		if p.Color != persp {
			pi += halfKPPieceKinds / 2
		}
		s := ((int(p.Sq.Rank)*8 + int(p.Sq.File)) ^ flip) & 63
		rows = append(rows, int32(pi*64+s))
	}
	accFeats(a, n.B1[:n.H], n.slotRows(slot), rows)
}

// applyDelta adds the rows of the pieces present in self but not in
// parent, and subtracts those present in parent but not in self, for one
// perspective. Kings are the conditioning variable and have no row.
func (n *HalfKPNet) applyDelta(a []float32, parent, self *halfKPAcc, persp board.Color, buckets int) {
	king := self.kings[persp]
	for c := 0; c < 2; c++ {
		owner := board.Color(c)
		for t := board.Pawn; t < board.King; t++ {
			changed := parent.pieces[c][t] ^ self.pieces[c][t]
			for changed != 0 {
				i := bits.TrailingZeros64(changed)
				changed &= changed - 1
				sq := board.Sq{File: int8(i % 8), Rank: int8(i / 8)}
				// Never !ok: the loop stops short of the king, the only
				// piece halfKPIndex refuses.
				f, _ := halfKPIndex(king, t, owner, sq, persp, buckets)
				sign := float32(-1)
				if self.pieces[c][t]&(1<<uint(i)) != 0 {
					sign = 1
				}
				n.addRow(a, int32(f), sign)
			}
		}
	}
}

// accDiff is the pieces that differ between a parent's snapshot and a
// child's, in the order applyDelta visits them: owner, then type, then
// square. It is perspective-free, so one diff serves both accumulators.
type accDiff struct {
	n     int
	piece [maxAccRows]uint8 // owner*5 + type, owner as the absolute colour
	sq    [maxAccRows]uint8 // rank*8 + file, unmirrored
	sign  [maxAccRows]float32
	// row is each entry's feature index inside its king slot's block, for
	// White's perspective then Black's: halfKPIndex without the slot term.
	// Always below halfKPPerKing, which is what lets the kernels skip their
	// bounds checks.
	row [2][maxAccRows]int32
}

// halfKPBlackPiece maps a diff entry's absolute owner*5 + type to the
// piece index Black's perspective uses, owners swapped. Sixteen entries,
// all below ten, so an index masked to four bits stays inside the block.
var halfKPBlackPiece = [16]int32{5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 9, 9, 9, 9, 9, 9}

// diffAcc fills d from the XOR of the two snapshots. It reports false when
// more pieces changed than one update batches.
//
// A move changes two to four bits in one to three of the ten non-king
// bitboards, so visiting all ten with a branch each was mostly loop and
// mispredicted branches. The XORs are ORed first: a null move stops there,
// and otherwise a ten-bit mask of the changed words, built without
// branches, is walked lowest first, which is still owner, then type, then
// square. Each entry also gets its row in both perspectives: halfKPIndex
// unrolled, from Black's side the owners swap halves and the rank mirrors,
// which on a 0-63 square is XOR 56.
func diffAcc(parent, self *halfKPAcc, d *accDiff) bool {
	p, s := &parent.pieces, &self.pieces
	var x, now [16]uint64
	x[0], x[1], x[2], x[3], x[4] = p[0][0]^s[0][0], p[0][1]^s[0][1], p[0][2]^s[0][2], p[0][3]^s[0][3], p[0][4]^s[0][4]
	x[5], x[6], x[7], x[8], x[9] = p[1][0]^s[1][0], p[1][1]^s[1][1], p[1][2]^s[1][2], p[1][3]^s[1][3], p[1][4]^s[1][4]
	if x[0]|x[1]|x[2]|x[3]|x[4]|x[5]|x[6]|x[7]|x[8]|x[9] == 0 {
		d.n = 0
		return true
	}
	now[0], now[1], now[2], now[3], now[4] = s[0][0], s[0][1], s[0][2], s[0][3], s[0][4]
	now[5], now[6], now[7], now[8], now[9] = s[1][0], s[1][1], s[1][2], s[1][3], s[1][4]
	words := nonZero(x[0]) | nonZero(x[1])<<1 | nonZero(x[2])<<2 | nonZero(x[3])<<3 | nonZero(x[4])<<4 |
		nonZero(x[5])<<5 | nonZero(x[6])<<6 | nonZero(x[7])<<7 | nonZero(x[8])<<8 | nonZero(x[9])<<9
	k := 0
	for words != 0 {
		w := bits.TrailingZeros64(words) & 15
		words &= words - 1
		v := x[w]
		v1 := v & (v - 1)
		if v1&(v1-1) != 0 || k > maxAccRows-2 {
			// Three changes in one bitboard, or no room for two more
			// entries: one bit at a time, stopping at a full batch.
			for ; v != 0; v &= v - 1 {
				if k == maxAccRows {
					return false
				}
				d.put(k, w, bits.TrailingZeros64(v)&63, now[w])
				k++
			}
			continue
		}
		// One move changes one or two bits of a bitboard. Both entries are
		// written and k advances by the number of bits, so the loop over
		// bits and its mispredicted exit are gone; with one bit, the second
		// entry is overwritten by the next word or left past d.n.
		d.put(k, w, bits.TrailingZeros64(v)&63, now[w])
		d.put(k+1, w, bits.TrailingZeros64(v1)&63, now[w])
		k += 1 + int(nonZero(v1))
	}
	d.n = k
	return true
}

// put writes entry k: square i of non-king bitboard w (owner*5 + type)
// changed, and now is that bitboard in the child, which says the sign.
func (d *accDiff) put(k, w, i int, now uint64) {
	d.piece[k] = uint8(w)
	d.sq[k] = uint8(i)
	d.sign[k] = diffSign[(now>>uint(i))&1]
	d.row[0][k] = int32(min(w, 9)*64 + i)
	d.row[1][k] = halfKPBlackPiece[w&15]*64 + int32(i^56)
}

// diffSign is a diff entry's row sign: -1 for a piece that left, +1 for one
// that arrived.
var diffSign = [2]float32{-1, 1}

// nonZero is 1 when x is not zero, 0 when it is, without a branch.
func nonZero(x uint64) uint64 { return (x | -x) >> 63 }

// applyDiff writes src plus d's rows into dst in one pass, for the
// perspective persp whose king sits in slot. The index is halfKPIndex's,
// unrolled: from Black's side the owners swap halves and the rank mirrors,
// which on a 0-63 index is XOR 56.
func (n *HalfKPNet) applyDiff(dst, src []float32, d *accDiff, persp board.Color, slot int) {
	h := n.H
	var rows [maxAccRows][]float32
	base := slot * halfKPPerKing
	half := halfKPPieceKinds / 2
	for j := 0; j < d.n; j++ {
		p, s := int(d.piece[j]), int(d.sq[j])
		if persp == board.Black {
			s ^= 56
			if p >= half {
				p -= half
			} else {
				p += half
			}
		}
		col := (base + p*64 + s) * h
		rows[j] = n.W1[col : col+h : col+h]
	}
	accRows(dst[:h:h], src[:h:h], &rows, &d.sign, d.n)
}

// fusedDelta is one perspective's incremental update on its own: the diff
// and the batched rows refresh applies, false when the move changes more
// rows than one update batches.
func (n *HalfKPNet) fusedDelta(dst, src []float32, parent, self *halfKPAcc, persp board.Color, buckets int) bool {
	var d accDiff
	if !diffAcc(parent, self, &d) {
		return false
	}
	n.applyDiff(dst, src, &d, persp, perspectiveKingSlot(self.kings[persp], persp, buckets))
	return true
}

// perspectiveKingSlot is the king slot halfKPIndex conditions on, with
// the rank mirrored for Black the way halfKPIndex mirrors it.
func perspectiveKingSlot(king board.Sq, persp board.Color, buckets int) int {
	if persp == board.Black {
		king = board.Sq{File: king.File, Rank: 7 - king.Rank}
	}
	return kingSlot(king, buckets)
}

// addRow adds (sign +1) or subtracts (sign -1) one feature's first-layer
// row into a, unrolled by eight like Evaluate's loop.
func (n *HalfKPNet) addRow(a []float32, f int32, sign float32) {
	h := n.H
	col := int(f) * h
	w := n.W1[col : col+h : col+h]
	a = a[:h:h]
	i := 0
	for ; i+8 <= h; i += 8 {
		a8 := a[i : i+8 : i+8]
		w8 := w[i : i+8 : i+8]
		a8[0] += sign * w8[0]
		a8[1] += sign * w8[1]
		a8[2] += sign * w8[2]
		a8[3] += sign * w8[3]
		a8[4] += sign * w8[4]
		a8[5] += sign * w8[5]
		a8[6] += sign * w8[6]
		a8[7] += sign * w8[7]
	}
	for ; i < h; i++ {
		a[i] += sign * w[i]
	}
}

// output is everything above a valid accumulator: the same layers
// Evaluate runs, reading the incrementally maintained halves instead of
// ones it computed itself. The accumulator update knows nothing about
// what sits on top of it and did not change when the second layer
// arrived.
func (n *HalfKPNet) output(acc *halfKPAcc) float64 {
	h := n.H
	return n.head(acc.acc[0][:h:h], acc.acc[1][:h:h])
}

// EvaluateWith is Evaluate with the accumulator taken from, and left in,
// self, derived from parent when parent is valid.
func (n *HalfKPNet) EvaluateWith(b *board.Board, self, parent *halfKPAcc, st *halfKPAccStats) float64 {
	if !self.valid {
		n.refresh(b, self, parent, st)
	}
	if !self.valid {
		return n.Evaluate(b)
	}
	return n.output(self)
}
