package engine

import (
	"encoding/json"
	"fmt"
	"math"
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
// perspectives, concatenated, then one hidden-to-output layer.
//
//	40960 -> H  (shared, applied twice)
//	2H    -> 1
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
			for i := 0; i < h; i++ {
				a[i] += w[i]
			}
		}
	}

	out := n.B2
	for i := 0; i < 2*h; i++ {
		a := acc[i]
		if a < 0 {
			a = 0
		} else if a > 1 {
			a = 1
		}
		out += n.W2[i] * a
	}
	if n.Sigmoid {
		k := n.K
		if k == 0 {
			k = 0.30
		}
		return probabilityToPawns(float64(out), k)
	}
	return float64(out * n.Scale)
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
	if len(n.B1) != n.H || len(n.W2) != 2*n.H {
		return nil, fmt.Errorf("halfkp %s: biases %d and output weights %d do not match "+
			"%d hidden units", path, len(n.B1), len(n.W2), n.H)
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
