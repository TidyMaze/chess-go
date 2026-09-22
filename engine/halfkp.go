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
	file := s.File
	if file > 3 {
		file = 7 - file
	}
	return (s.Rank/4)*4 + file
}

// kingCanonicalSquare is the 32-way version: every rank kept, files
// mirrored about the centre.
func kingCanonicalSquare(s Sq) int {
	file := s.File
	if file > 3 {
		file = 7 - file
	}
	return s.Rank*4 + file
}

// kingSlot picks between them. buckets is 8 or 32; anything else is
// treated as 8, which is what every network written before this existed
// was trained with.
func kingSlot(s Sq, buckets int) int {
	if buckets == halfKPKingSquares {
		return kingCanonicalSquare(s)
	}
	return kingBucket(s)
}

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
	s := ps.Rank*8 + ps.File
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

// clip01 is the clipped ReLU both layers use.
func clip01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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

// head runs everything above the accumulator: the clipped ReLU on both
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

		i := 0
		for ; i+4 <= h; i += 4 {
			out += wOwn[i] * clip01(own[i])
			out += wOwn[i+1] * clip01(own[i+1])
			out += wOwn[i+2] * clip01(own[i+2])
			out += wOwn[i+3] * clip01(own[i+3])
		}
		for ; i < h; i++ {
			out += wOwn[i] * clip01(own[i])
		}

		i = 0
		for ; i+4 <= h; i += 4 {
			out += wOpp[i] * clip01(opp[i])
			out += wOpp[i+1] * clip01(opp[i+1])
			out += wOpp[i+2] * clip01(opp[i+2])
			out += wOpp[i+3] * clip01(opp[i+3])
		}
		for ; i < h; i++ {
			out += wOpp[i] * clip01(opp[i])
		}
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
	pieces [2][6]uint64
	kings  [2]board.Sq
	acc    [2][maxHalfKPHidden]float32
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
	for c := 0; c < 2; c++ {
		for t := board.Pawn; t <= board.King; t++ {
			self.pieces[c][t] = b.PieceBitboard(board.Color(c), t)
		}
	}
	self.kings = [2]board.Sq{b.KingSquare(board.White), b.KingSquare(board.Black)}
	for side, persp := range [2]board.Color{board.White, board.Black} {
		a := self.acc[side][:h]
		if parent != nil && parent.valid &&
			perspectiveKingSlot(parent.kings[side], persp, buckets) == perspectiveKingSlot(self.kings[side], persp, buckets) {
			if !n.fusedDelta(a, parent.acc[side][:h], parent, self, persp, buckets) {
				copy(a, parent.acc[side][:h])
				n.applyDelta(a, parent, self, persp, buckets)
			}
			if st != nil {
				st.incremental++
			}
			continue
		}
		copy(a, n.B1)
		var buf [32]int32
		for _, f := range AppendHalfKPFeaturesN(buf[:0], b, persp, buckets) {
			n.addRow(a, f, 1)
		}
		if st != nil {
			st.full++
		}
	}
	self.valid = true
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
				sq := board.Sq{File: i % 8, Rank: i / 8}
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

// fusedDelta writes src plus the changed rows into dst in one pass, in the
// order applyDelta visits them, so each unit is rounded identically. It
// reports false when a move changes more rows than it batches.
func (n *HalfKPNet) fusedDelta(dst, src []float32, parent, self *halfKPAcc, persp board.Color, buckets int) bool {
	const maxRows = 4
	var rows [maxRows][]float32
	var signs [maxRows]float32
	k := 0
	king := self.kings[persp]
	h := n.H
	for c := 0; c < 2; c++ {
		owner := board.Color(c)
		for t := board.Pawn; t < board.King; t++ {
			changed := parent.pieces[c][t] ^ self.pieces[c][t]
			for changed != 0 {
				if k == maxRows {
					return false
				}
				i := bits.TrailingZeros64(changed)
				changed &= changed - 1
				f, _ := halfKPIndex(king, t, owner, board.Sq{File: i % 8, Rank: i / 8}, persp, buckets)
				col := f * h
				rows[k] = n.W1[col : col+h : col+h]
				signs[k] = -1
				if self.pieces[c][t]&(1<<uint(i)) != 0 {
					signs[k] = 1
				}
				k++
			}
		}
	}
	dst, src = dst[:h:h], src[:h:h]
	switch k {
	case 0:
		copy(dst, src)
	case 2:
		w0, w1, s0, s1 := rows[0], rows[1], signs[0], signs[1]
		for i := range dst {
			v := src[i] + s0*w0[i]
			dst[i] = v + s1*w1[i]
		}
	default:
		copy(dst, src)
		for j := 0; j < k; j++ {
			w, s := rows[j], signs[j]
			for i := range dst {
				dst[i] += s * w[i]
			}
		}
	}
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
