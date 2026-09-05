package engine

import (
	"encoding/json"
	"os"

	"chess/board"
)

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
// maxHalfKPHidden bounds the accumulator so it can live on the stack.
const maxHalfKPHidden = 128

const (
	halfKPPieceKinds = 10 // 5 piece types x 2 colours, kings excluded
	halfKPPerKing    = halfKPPieceKinds * 64
	HalfKPInputs     = 64 * halfKPPerKing // 40,960
)

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
func halfKPIndex(kingSq board.Sq, pt board.PieceType, owner board.Color, sq board.Sq, perspective board.Color) (int, bool) {
	pi, ok := halfKPPieceIndex(pt, owner, perspective)
	if !ok {
		return 0, false
	}
	ks, ps := kingSq, sq
	if perspective == board.Black {
		ks = board.Sq{File: ks.File, Rank: 7 - ks.Rank}
		ps = board.Sq{File: ps.File, Rank: 7 - ps.Rank}
	}
	k := ks.Rank*8 + ks.File
	s := ps.Rank*8 + ps.File
	return k*halfKPPerKing + pi*64 + s, true
}

// AppendHalfKPFeatures writes the active input indices for one
// perspective. At most 30 of 40,960 are ever set, which is what makes
// the first layer affordable: it costs one column addition per piece.
func AppendHalfKPFeatures(dst []int32, b *board.Board, perspective board.Color) []int32 {
	king := b.KingSquare(perspective)
	var buf [32]board.ColoredPiece
	for _, p := range b.AppendAllPieces(buf[:0]) {
		if i, ok := halfKPIndex(king, p.Type, p.Color, p.Sq, perspective); ok {
			dst = append(dst, int32(i))
		}
	}
	return dst
}

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
}

// Evaluate returns the score in pawns from White's point of view.
func (n *HalfKPNet) Evaluate(b *board.Board) float64 {
	if n == nil || n.H == 0 || len(n.W1) != HalfKPInputs*n.H {
		return 0
	}
	h := n.H
	// Stack array, not an allocation. This runs once per evaluated
	// position, which a search does millions of times per move, so an
	// allocation here would dominate everything else the engine does.
	var accArr [2 * maxHalfKPHidden]float32
	if h > maxHalfKPHidden {
		return 0
	}
	acc := accArr[: 2*h : 2*h]
	copy(acc[:h], n.B1)
	copy(acc[h:], n.B1)

	var buf [32]int32
	for side, persp := range [2]board.Color{board.White, board.Black} {
		off := side * h
		for _, f := range AppendHalfKPFeatures(buf[:0], b, persp) {
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
	return &n, nil
}

func (n *HalfKPNet) Save(path string) error {
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
