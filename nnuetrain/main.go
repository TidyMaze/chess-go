// Command nnuetrain fits the small evaluation network in engine/nnue.go
// to Stockfish's judgement of positions.
//
// One hidden layer needs no framework: the gradients are three lines, and
// writing them here keeps the training and the inference in the same
// language and the same repository, so they cannot drift apart in the way
// an exported model and a hand-written forward pass do.
//
// The input is sparse (at most 32 of 768 features are set), which shapes
// both halves: the forward pass adds one weight column per piece, and the
// backward pass only touches those same columns.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

	"chess/board"
	"chess/engine"
	"chess/game"
)

const inputs = 768

// hidden is a variable, not a constant: the first networks underfit
// (training error as high as held-out error, ~2 pawns RMS either way),
// and capacity is the first thing to rule out when that happens.
var hidden = 64

type example struct {
	features []int32 // indices of the set inputs, at most 32
	target   float64 // what the network should output, in pawns
}

func featureIndex(c board.Color, pt board.PieceType, sq board.Sq) int32 {
	return int32((int(c)*6+int(pt))*64 + sq.Rank*8 + sq.File)
}

const clampPawns = 8.0

// handEval must be exactly the evaluation the network's output will be
// added to at play time, or the network learns to correct a function the
// engine is not using. The first version based the residual on the
// Texel-tuned weights while the engine plays with the default ones, so
// the network was also being asked to undo the tuned set's inflated
// piece values (a queen at 12 rather than 9).
var handEval = &engine.Eval{Weights: engine.DefaultWeights(), UsePST: true,
	Tapered: true, Structure: true}

func loadExamples(path string, limit int, residual bool) []example {
	f, err := os.Open(path)
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<22), 1<<22)

	var out []example
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var rec struct {
			FEN   string  `json:"fen"`
			Score float64 `json:"score"`
			Mate  bool    `json:"mate"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil || rec.Mate {
			continue
		}
		g, err := game.ParseFEN(rec.FEN)
		if err != nil {
			continue
		}
		// Stockfish reports from the side to move; everything here is
		// from White's point of view.
		score := rec.Score
		if g.Turn == board.Black {
			score = -score
		}
		var feats []int32
		var buf [16]board.PieceAtSquare
		for _, c := range [2]board.Color{board.White, board.Black} {
			for _, ps := range g.Board.AppendPiecesOf(buf[:0], c) {
				feats = append(feats, featureIndex(c, ps.Type, ps.Sq))
			}
		}
		// Target in pawns, not win probability.
		//
		// The first version fitted sigmoid(K*out) to sigmoid(K*sf) and
		// scored -700 Elo. The sanity check said why: the network read
		// "Black is a rook up" as -1.38 instead of about -5, because the
		// sigmoid saturates. At K = 0.3, +5 pawns and +10 pawns are 0.82
		// and 0.95, so the loss barely cares which, and the network never
		// learned what a rook is worth. Regressing on the score itself
		// keeps material differences in the loss where they belong.
		//
		// Clamped because positions already winning by more than this are
		// decided, and letting them dominate a squared error would spend
		// all the capacity on positions the search wins anyway.
		if score > clampPawns {
			score = clampPawns
		} else if score < -clampPawns {
			score = -clampPawns
		}
		target := score
		if residual {
			// The hand-written evaluation already counts material
			// correctly. Learning only what it gets wrong means material
			// cannot be unlearned, which is the failure above.
			hand := engine.PositionScoreEval(&g.Board, board.White, handEval)
			target = score - hand
		}
		out = append(out, example{features: feats, target: target})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

type net struct {
	w1 []float32
	b1 []float32
	w2 []float32
	b2 float32
}

func newNet(rng *rand.Rand) *net {
	n := &net{
		w1: make([]float32, inputs*hidden),
		b1: make([]float32, hidden),
		w2: make([]float32, hidden),
	}
	// Small random weights. The scale matters more than usual here: the
	// hidden activation is clipped at 1, so weights large enough to
	// saturate every unit at the start would give zero gradient and the
	// network would never move.
	for i := range n.w1 {
		n.w1[i] = float32(rng.NormFloat64() * 0.01)
	}
	for i := range n.w2 {
		n.w2[i] = float32(rng.NormFloat64() * 0.1)
	}
	return n
}

// forward returns the pre-activation hidden layer, the activated hidden
// layer, and the raw output.
func (n *net) forward(feats []int32, acc, act *[]float32) float32 {
	copy(*acc, n.b1)
	for _, f := range feats {
		col := int(f) * hidden
		w := n.w1[col : col+hidden : col+hidden]
		for i := 0; i < hidden; i++ {
			(*acc)[i] += w[i]
		}
	}
	out := n.b2
	for i := 0; i < hidden; i++ {
		a := (*acc)[i]
		if a < 0 {
			a = 0
		} else if a > 1 {
			a = 1
		}
		(*act)[i] = a
		out += n.w2[i] * a
	}
	return out
}

func (n *net) loss(data []example) float64 {
	acc, act := make([]float32, hidden), make([]float32, hidden)
	total := 0.0
	for _, e := range data {
		out := n.forward(e.features, &acc, &act)
		d := float64(out) - e.target
		total += d * d
	}
	return total / float64(len(data))
}

func main() {
	labels := flag.String("labels", "tuning_labels.jsonl", "Stockfish-labelled positions")
	limit := flag.Int("limit", 0, "cap examples (0 = all)")
	epochs := flag.Int("epochs", 30, "training epochs")
	lr := flag.Float64("lr", 0.05, "learning rate")
	residual := flag.Bool("residual", true, "learn the correction to the hand-written evaluation, not the whole score")
	holdout := flag.Float64("holdout", 0.15, "fraction held back")
	seed := flag.Int64("seed", 3, "shuffle and init seed")
	out := flag.String("out", "net.json", "where to write the trained network")
	hiddenFlag := flag.Int("hidden", 64, "hidden units")
	flag.Parse()
	hidden = *hiddenFlag

	rng := rand.New(rand.NewSource(*seed))
	all := loadExamples(*labels, *limit, *residual)
	if len(all) < 1000 {
		fmt.Printf("only %d examples; run label first\n", len(all))
		return
	}
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	split := int(float64(len(all)) * (1 - *holdout))
	train, test := all[:split], all[split:]
	fmt.Printf("%d examples (%d train, %d held out), %d inputs, %d hidden\n",
		len(all), len(train), len(test), inputs, hidden)

	n := newNet(rng)
	fmt.Printf("epoch  train      held out\n")
	fmt.Printf("start  %.6f   %.6f\n", n.loss(train), n.loss(test))

	bestTest := math.Inf(1)
	var bestNet net
	t0 := time.Now()
	acc, act := make([]float32, hidden), make([]float32, hidden)
	order := make([]int, len(train))
	for i := range order {
		order[i] = i
	}

	for epoch := 1; epoch <= *epochs; epoch++ {
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		eta := float32(*lr)
		for _, idx := range order {
			e := train[idx]
			out := n.forward(e.features, &acc, &act)
			// d(loss)/d(out) for loss = (out - target)^2, in pawns.
			dOut := float32(2 * (float64(out) - e.target))

			// Output layer.
			for i := 0; i < hidden; i++ {
				grad := dOut * act[i]
				// Gradient into the hidden unit, zero where the clipped
				// ReLU is flat: a saturated unit passes nothing back.
				var dAct float32
				if acc[i] > 0 && acc[i] < 1 {
					dAct = dOut * n.w2[i]
				}
				n.w2[i] -= eta * grad
				act[i] = dAct // reuse as the hidden gradient
			}
			n.b2 -= eta * dOut

			// Hidden layer. Only the columns for pieces actually on the
			// board receive gradient, which is why this is affordable.
			for i := 0; i < hidden; i++ {
				n.b1[i] -= eta * act[i]
			}
			for _, f := range e.features {
				col := int(f) * hidden
				w := n.w1[col : col+hidden : col+hidden]
				for i := 0; i < hidden; i++ {
					w[i] -= eta * act[i]
				}
			}
		}

		trainLoss, testLoss := n.loss(train), n.loss(test)
		marker := ""
		if testLoss < bestTest {
			// Keep the best network by held-out loss, not the last one:
			// the last epoch is not automatically the best and this is the
			// cheapest possible guard against training past the point of
			// usefulness.
			bestTest = testLoss
			bestNet = *n
			bestNet.w1 = append([]float32(nil), n.w1...)
			bestNet.b1 = append([]float32(nil), n.b1...)
			bestNet.w2 = append([]float32(nil), n.w2...)
			marker = "  <- best"
		}
		fmt.Printf("%5d  %.6f   %.6f%s\n", epoch, trainLoss, testLoss, marker)
	}
	fmt.Printf("trained in %.0fs\n", time.Since(t0).Seconds())

	saved := &engine.Net{
		W1: bestNet.w1, B1: bestNet.b1, W2: bestNet.w2, B2: bestNet.b2,
		Scale: 1, Residual: *residual,
	}
	if err := saved.Save(*out); err != nil {
		fmt.Println("save:", err)
		return
	}
	// Variance of the target: what a network that always guessed the mean
	// would score. Anything close to this has learned nothing.
	mean := 0.0
	for _, e := range test {
		mean += e.target
	}
	mean /= float64(len(test))
	variance := 0.0
	for _, e := range test {
		variance += (e.target - mean) * (e.target - mean)
	}
	variance /= float64(len(test))
	fmt.Printf("\nheld-out MSE %.4f pawns^2 (RMS %.3f pawns); always guessing the mean scores %.4f\n",
		bestTest, math.Sqrt(bestTest), variance)
	fmt.Printf("wrote %s (residual=%v)\n", *out, *residual)
}
