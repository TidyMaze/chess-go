package engine

import (
	"encoding/json"
	"math"
	"os"

	"chess/board"
)

// A small neural network evaluation, in the style of NNUE (Nasu, 2018;
// adopted by Stockfish in 2020).
//
// Why this and not another hand-written term. Measured on 2026-09-05,
// agreement with a depth-14 Stockfish stops improving past depth 4 and a
// material-only evaluation actively gets worse with depth, so the
// evaluation is what limits this engine, not the search. Fitting the
// existing hand-written terms harder has now been tried twice: against
// game outcomes (-16 +/- 48 Elo) and against Stockfish scores (+20 +/- 26
// over 700 games). Both are within or barely outside their own margins.
// The shape of the function is the limit, not its coefficients: a linear
// sum of piece values and table lookups cannot express "this knight is
// good *because* those pawns are fixed there".
//
// The architecture is deliberately the smallest thing that is genuinely
// non-linear: 768 binary inputs (12 piece-and-colour types x 64 squares),
// one hidden layer with clipped ReLU, one output in pawns from White's
// point of view.
//
// The reason NNUE is affordable at all is that the input is sparse. At
// most 32 of the 768 inputs are ever set, so the hidden layer costs 32
// column additions rather than a 768xH matrix multiply.
const nnueInputs = 768

// Net is a trained network. Weights are flat and float32: this is read
// once per evaluated position, which the search does millions of times.
type Net struct {
	W1 []float32 `json:"w1"` // nnueInputs * nnueHidden, feature-major
	B1 []float32 `json:"b1"` // nnueHidden
	W2 []float32 `json:"w2"` // nnueHidden
	B2 float32   `json:"b2"`
	// Scale converts the network's output into pawns. Training works in
	// win-probability space, so the raw output is in whatever units the
	// sigmoid wanted; this puts it back on the same scale as the rest of
	// the engine, where 1.0 is a pawn.
	Scale float32 `json:"scale"`
	// Residual marks a network trained to correct the hand-written
	// evaluation rather than replace it. The two are not interchangeable
	// and swapping them silently would look like a bad network rather
	// than a wiring mistake, so the file says which it is.
	Residual bool `json:"residual"`
}

// featureIndex maps a piece on a square to its input neuron. Colour is
// the outer dimension so that all of one side's features are contiguous,
// which is what makes an incremental update cheap to write later.
func featureIndex(c board.Color, pt board.PieceType, sq board.Sq) int {
	return (int(c)*6+int(pt))*64 + sq.Rank*8 + sq.File
}

// Evaluate returns the network's score for the position, in pawns and
// from White's point of view.
func (n *Net) Evaluate(b *board.Board) float64 {
	if n == nil || len(n.B1) == 0 || len(n.W1) != nnueInputs*len(n.B1) {
		return 0
	}
	h := len(n.B1)
	// acc holds the hidden layer before activation. Starting from the
	// bias and adding one column per piece is the whole trick: the cost
	// is the number of pieces, not the number of inputs.
	acc := make([]float32, h)
	copy(acc, n.B1)

	var buf [16]board.PieceAtSquare
	for _, c := range [2]board.Color{board.White, board.Black} {
		for _, ps := range b.AppendPiecesOf(buf[:0], c) {
			col := featureIndex(c, ps.Type, ps.Sq) * h
			w := n.W1[col : col+h : col+h]
			for i := 0; i < h; i++ {
				acc[i] += w[i]
			}
		}
	}

	out := n.B2
	for i := 0; i < h; i++ {
		// Clipped ReLU, as NNUE uses: the clip keeps one loud feature from
		// dominating the sum and keeps activations in a fixed range.
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

// LoadNet reads a trained network from disk.
func LoadNet(path string) (*Net, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var n Net
	if err := json.Unmarshal(data, &n); err != nil {
		return nil, err
	}
	if n.Scale == 0 {
		n.Scale = 1
	}
	return &n, nil
}

// Save writes the network to disk.
func (n *Net) Save(path string) error {
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Sigmoid maps a score in pawns to a win probability, using the same K
// the tuner fits. Shared so training and measurement cannot drift apart.
func Sigmoid(pawns, k float64) float64 { return 1 / (1 + math.Exp(-k*pawns)) }
