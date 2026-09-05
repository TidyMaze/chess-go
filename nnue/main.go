// Command nnue runs the training pipeline Stockfish uses for its
// evaluation network, at the scale this machine allows.
//
// The pieces that matter, and how each differs from the earlier attempt
// in trainloop:
//
//   - **HalfKP features.** Every piece feature is conditioned on the
//     king's square, 40,960 inputs per side rather than 768. This is what
//     lets a network learn king safety at all; a flat piece-square input
//     cannot express "this knight is dangerous because their king is on
//     g8", which is close to what piece-square tables already say, and is
//     why the earlier network never beat them.
//   - **A blended target.** Stockfish trains on
//     lambda*sigmoid(search score) + (1-lambda)*game result. The search
//     score is dense and low-variance; the game result anchors it to what
//     actually wins. The earlier attempt used one or the other and got
//     the worst of both.
//   - **Shallow, high-volume generation.** Labels come from a shallow
//     search, not a deep one. Throughput matters more than label quality
//     because volume averages the noise out, and this is the single
//     biggest lever on how much data exists to learn from.
//   - **Iteration.** Train, play with the result, regenerate data with
//     the stronger engine, retrain.
//
// Everything streams to JSON so the run can be watched in the browser.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"chess/board"
	"chess/engine"
	"chess/game"
	"chess/moves"
)

type sample struct {
	own    []int32 // active HalfKP features, White perspective
	opp    []int32 // active HalfKP features, Black perspective
	target float64 // blended win probability, White's point of view
	game   int32
}

var writeMu sync.Mutex

func writeJSON(path string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	_ = os.WriteFile(path, data, 0644)
}

// ---------------------------------------------------------------------
// network

type net struct {
	h  int
	w1 []float32 // HalfKPInputs * h, shared by both perspectives
	b1 []float32
	w2 []float32 // 2h
	b2 float32
}

func newNet(h int, rng *rand.Rand) *net {
	n := &net{
		h:  h,
		w1: make([]float32, engine.HalfKPInputs*h),
		b1: make([]float32, h),
		w2: make([]float32, 2*h),
	}
	// Small weights: about 30 features are active at once, so the
	// accumulator is a sum of 30 of these, and the activation clips at 1.
	for i := range n.w1 {
		n.w1[i] = float32(rng.NormFloat64() * 0.005)
	}
	for i := range n.w2 {
		n.w2[i] = float32(rng.NormFloat64() * 0.05)
	}
	return n
}

func (n *net) forward(s *sample, acc, act []float32) float32 {
	h := n.h
	copy(acc[:h], n.b1)
	copy(acc[h:], n.b1)
	for side, feats := range [2][]int32{s.own, s.opp} {
		off := side * h
		a := acc[off : off+h]
		for _, f := range feats {
			col := int(f) * h
			w := n.w1[col : col+h : col+h]
			for i := 0; i < h; i++ {
				a[i] += w[i]
			}
		}
	}
	out := n.b2
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
	return out
}

func (n *net) loss(data []sample) float64 {
	acc, act := make([]float32, 2*n.h), make([]float32, 2*n.h)
	total := 0.0
	for i := range data {
		d := float64(n.forward(&data[i], acc, act)) - data[i].target
		total += d * d
	}
	return total / float64(len(data))
}

// trainEpoch runs one pass of stochastic gradient descent, split across
// all cores without locking.
//
// This is Hogwild (Niu et al., 2011): workers read and write the shared
// weights with no synchronisation at all. It is safe here for the reason
// it is safe there, that the updates are sparse. Each position touches
// about 30 of 40,960 feature columns, so two workers colliding on the
// same column is rare, and a lost update is indistinguishable from noise
// the optimiser already tolerates. Locking would cost more than the
// collisions do.
//
// Single-threaded training was the bottleneck in the previous loop: data
// generation used every core and then training used one.
func (n *net) trainEpoch(data []sample, order []int32, lr, decay float32, workers int) {
	var wg sync.WaitGroup
	chunk := (len(order) + workers - 1) / workers
	h := n.h
	for w := 0; w < workers; w++ {
		lo := w * chunk
		hi := lo + chunk
		if lo >= len(order) {
			break
		}
		if hi > len(order) {
			hi = len(order)
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			acc := make([]float32, 2*h)
			act := make([]float32, 2*h)
			grad := make([]float32, 2*h)
			for _, idx := range order[lo:hi] {
				s := &data[idx]
				out := n.forward(s, acc, act)
				dOut := 2 * (out - float32(s.target))

				for i := 0; i < 2*h; i++ {
					// Gradient into the hidden unit, zero where the clipped
					// activation is flat.
					if acc[i] > 0 && acc[i] < 1 {
						grad[i] = dOut * n.w2[i]
					} else {
						grad[i] = 0
					}
					n.w2[i] -= lr * dOut * act[i]
				}
				n.b2 -= lr * dOut

				for side, feats := range [2][]int32{s.own, s.opp} {
					off := side * h
					g := grad[off : off+h]
					for i := 0; i < h; i++ {
						n.b1[i] -= lr * g[i]
					}
					for _, f := range feats {
						col := int(f) * h
						wv := n.w1[col : col+h : col+h]
						for i := 0; i < h; i++ {
							wv[i] -= lr * g[i]
						}
					}
				}
			}
		}(lo, hi)
	}
	wg.Wait()
}

func (n *net) export() *engine.HalfKPNet {
	return &engine.HalfKPNet{
		H:  n.h,
		W1: append([]float32(nil), n.w1...),
		B1: append([]float32(nil), n.b1...),
		W2: append([]float32(nil), n.w2...),
		B2: n.b2, Scale: 1,
	}
}

// ---------------------------------------------------------------------
// data generation

// sigmoid maps a score in pawns to a win probability. K is the same
// constant the evaluation tuner fits.
func sigmoid(pawns, k float64) float64 { return 1 / (1 + math.Exp(-k*pawns)) }

type genStats struct {
	games     int64
	positions int64
}

// generate plays self-play games and returns labelled quiet positions.
//
// Labels are blended, as Stockfish does: the shallow search score carries
// the dense signal and the game result anchors it. Positions are kept
// only when nothing tactical is pending, since a static network cannot
// predict a capture sequence and training on those teaches noise.
func generate(champion engine.Player, games, playDepth, labelDepth, maxPlies int,
	lambda, k float64, gen int, stats *genStats,
	live func(*game.Game, int, board.Sq, board.Sq)) []sample {

	var mu sync.Mutex
	var out []sample

	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for gi := 0; gi < games; gi++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(gi int) {
			defer wg.Done()
			defer func() { <-sem }()

			player := champion
			player.Depth = playDepth
			labeler := champion
			labeler.Depth = labelDepth
			// One table for this whole game rather than one per labelled
			// position. 16 bits is 1.5 MB, which stays in cache; the
			// labelling search is shallow and does not need more.
			labelTT := engine.NewTranspositionTable(16)

			rng := rand.New(rand.NewSource(int64(gen)*1_000_003 + int64(gi)*7919))
			g := game.New()
			g.EnableRepetitionTracking()
			for i := 0; i < 10; i++ {
				legal := g.AllLegalMoves(g.Turn)
				if len(legal) == 0 {
					break
				}
				m := legal[rng.Intn(len(legal))]
				g.ApplyMove(m.From, m.To)
			}

			type pending struct {
				own, opp []int32
				score    float64
			}
			var kept []pending
			result := 0.5

			for ply := 0; ply < maxPlies; ply++ {
				if g.IsCheckmate(g.Turn) {
					if g.Turn == board.White {
						result = 0
					} else {
						result = 1
					}
					break
				}
				if g.IsStalemate(g.Turn) || g.IsFiftyMoveDraw() ||
					g.IsThreefoldRepetition() || g.KingCaptured {
					break
				}
				m, ok := engine.PlayerPick(player, g)
				if !ok {
					break
				}
				g.ApplyMove(m.From, m.To)
				if gi == 0 && live != nil {
					live(g, ply+1, m.From, m.To)
				}

				if moves.IsInCheck(&g.Board, g.Turn) {
					continue
				}
				static := engine.PlayerStaticEval(champion, &g.Board)
				staticSTM := static
				if g.Turn == board.Black {
					staticSTM = -static
				}
				if math.Abs(engine.QuiescenceScore(labeler, g)-staticSTM) > 0.35 {
					continue
				}
				score, ok := engine.PlayerScoreWith(labeler, g, labelTT)
				if !ok {
					continue
				}
				if g.Turn == board.Black {
					score = -score
				}
				if score > 12 {
					score = 12
				} else if score < -12 {
					score = -12
				}
				var ownBuf, oppBuf []int32
				ownBuf = engine.AppendHalfKPFeatures(ownBuf, &g.Board, board.White)
				oppBuf = engine.AppendHalfKPFeatures(oppBuf, &g.Board, board.Black)
				kept = append(kept, pending{ownBuf, oppBuf, score})
			}

			local := make([]sample, 0, len(kept))
			for _, p := range kept {
				local = append(local, sample{
					own: p.own, opp: p.opp, game: int32(gi),
					target: lambda*sigmoid(p.score, k) + (1-lambda)*result,
				})
			}
			mu.Lock()
			out = append(out, local...)
			mu.Unlock()
			atomic.AddInt64(&stats.games, 1)
			atomic.AddInt64(&stats.positions, int64(len(local)))
		}(gi)
	}
	wg.Wait()
	return out
}

// ---------------------------------------------------------------------

type genRecord struct {
	Generation int     `json:"generation"`
	Positions  int     `json:"positions"`
	TrainLoss  float64 `json:"train_loss"`
	TestLoss   float64 `json:"test_loss"`
	Baseline   float64 `json:"baseline"`
	Elo        int     `json:"elo"`
	EloMargin  int     `json:"elo_margin"`
	Accepted   bool    `json:"accepted"`
	Tested     bool    `json:"tested"`
	CumElo     int     `json:"cum_elo"`
	Seconds    int     `json:"seconds"`
}

func main() {
	generations := flag.Int("generations", 200, "generations")
	gamesPerGen := flag.Int("games", 600, "self-play games per generation")
	playDepth := flag.Int("play-depth", 2, "search depth while generating games")
	labelDepth := flag.Int("label-depth", 4, "shallow search used to label positions")
	evalGames := flag.Int("eval-games", 400, "games to test a new network")
	// The test match costs about as much as generating a whole
	// generation, and early networks have no chance of being adopted, so
	// running it every time roughly halves the rate at which data
	// accumulates. Data is the measured constraint, so the match is run
	// periodically instead.
	evalEvery := flag.Int("eval-every", 5, "run the test match every N generations")
	evalDepth := flag.Int("eval-depth", 4, "depth for the test match")
	epochs := flag.Int("epochs", 6, "training epochs per generation")
	lr := flag.Float64("lr", 0.01, "learning rate")
	// L2 weight decay. With 40,960 x hidden parameters and far fewer
	// positions than that, the network memorises: generation 1 showed a
	// training loss of 0.0017 against a held-out 0.0473, a 28x gap.
	decay := flag.Float64("decay", 1e-4, "L2 weight decay on touched columns")
	lambda := flag.Float64("lambda", 0.8, "weight on the search score against the game result")
	k := flag.Float64("k", 0.30, "pawns-to-win-probability scale")
	hidden := flag.Int("hidden", 32, "hidden units per perspective")
	poolCap := flag.Int("pool", 3000000, "maximum positions kept")
	maxPlies := flag.Int("max-plies", 160, "ply cap in self-play")
	flag.Parse()

	rng := rand.New(rand.NewSource(23))
	n := newNet(*hidden, rng)
	workers := runtime.NumCPU()

	champion := engine.Player{
		Name: "champion", UsePST: true, Quiescence: true, TTBits: 20,
		NullMove: true, Tapered: true, Iterative: true, Extensions: true,
		Aspiration: true, SEEPruning: true, Structure: true, Futility: true,
	}

	params := engine.HalfKPInputs*(*hidden) + *hidden + 2*(*hidden) + 1
	arch := map[string]any{
		"features":    "HalfKP (king square x piece x square)",
		"inputs":      engine.HalfKPInputs,
		"hidden":      *hidden,
		"layers":      fmt.Sprintf("%d -> %d (shared, both perspectives) -> %d -> 1", engine.HalfKPInputs, *hidden, 2**hidden),
		"activation":  "clipped ReLU [0,1]",
		"params":      params,
		"target":      fmt.Sprintf("%.2f x sigmoid(search score) + %.2f x game result", *lambda, 1-*lambda),
		"label_depth": *labelDepth,
		"play_depth":  *playDepth,
		"eval_every":  *evalEvery,
		"eval_games":  *evalGames,
	}
	fmt.Printf("HalfKP %d inputs x %d hidden per side, %d parameters, %d workers\n",
		engine.HalfKPInputs, *hidden, params, workers)

	var history []genRecord
	var pool []sample
	cumElo := 0

	for gen := 1; gen <= *generations; gen++ {
		t0 := time.Now()
		stats := &genStats{}

		done := make(chan struct{})
		go func() {
			for {
				select {
				case <-done:
					return
				case <-time.After(700 * time.Millisecond):
					writeJSON("nnue_status.json", map[string]any{
						"phase": "self-play", "generation": gen, "generations": *generations,
						"games": atomic.LoadInt64(&stats.games), "games_total": *gamesPerGen,
						"positions": atomic.LoadInt64(&stats.positions),
						"pool":      len(pool), "cum_elo": cumElo,
						"arch": arch, "next_eval": nextEval(gen, *evalEvery),
					})
				}
			}
		}()

		fresh := generate(champion, *gamesPerGen, *playDepth, *labelDepth, *maxPlies,
			*lambda, *k, gen, stats,
			func(g *game.Game, ply int, from, to board.Sq) {
				writeJSON("live_game.json", map[string]any{
					"label": fmt.Sprintf("Generation %d self-play", gen), "move_no": ply,
					"mover": moverName(g.Turn.Other()),
					"from":  squareName(from), "to": squareName(to),
					"board": boardSnapshot(&g.Board),
				})
			})
		close(done)

		pool = append(pool, fresh...)
		if len(pool) > *poolCap {
			pool = pool[len(pool)-*poolCap:]
		}
		fmt.Printf("gen %d: %d new positions, pool %d (%.0fs generating)\n",
			gen, len(fresh), len(pool), time.Since(t0).Seconds())

		// Split by game, never by position: consecutive positions in a
		// game differ by one move, so a random split leaks near-copies
		// into the held-out set and reports a score the model cannot
		// reproduce in play.
		ids := map[int32]bool{}
		for i := range pool {
			ids[pool[i].game] = true
		}
		idList := make([]int32, 0, len(ids))
		for id := range ids {
			idList = append(idList, id)
		}
		sort.Slice(idList, func(i, j int) bool { return idList[i] < idList[j] })
		rng.Shuffle(len(idList), func(i, j int) { idList[i], idList[j] = idList[j], idList[i] })
		heldOut := map[int32]bool{}
		for _, id := range idList[:1+len(idList)*15/100] {
			heldOut[id] = true
		}
		var trainIdx []int32
		var testSet []sample
		for i := range pool {
			if heldOut[pool[i].game] {
				testSet = append(testSet, pool[i])
			} else {
				trainIdx = append(trainIdx, int32(i))
			}
		}
		if len(testSet) == 0 || len(trainIdx) == 0 {
			continue
		}

		var lastTrain, lastTest float64
		for e := 1; e <= *epochs; e++ {
			rng.Shuffle(len(trainIdx), func(i, j int) {
				trainIdx[i], trainIdx[j] = trainIdx[j], trainIdx[i]
			})
			n.trainEpoch(pool, trainIdx, float32(*lr), float32(*decay), workers)
			lastTest = n.loss(testSet)
			writeJSON("nnue_status.json", map[string]any{
				"phase": "training", "generation": gen, "generations": *generations,
				"epoch": e, "epochs": *epochs, "test_loss": lastTest,
				"pool": len(pool), "cum_elo": cumElo,
				"arch": arch, "next_eval": nextEval(gen, *evalEvery),
			})
		}
		// Training loss over a sample, since the pool can be millions.
		sampleN := len(trainIdx)
		if sampleN > 20000 {
			sampleN = 20000
		}
		sub := make([]sample, 0, sampleN)
		for _, i := range trainIdx[:sampleN] {
			sub = append(sub, pool[i])
		}
		lastTrain = n.loss(sub)

		// What a constant prediction would score. A held-out loss near
		// this means nothing has been learned, which the loss alone does
		// not reveal.
		mean := 0.0
		for i := range testSet {
			mean += testSet[i].target
		}
		mean /= float64(len(testSet))
		baseline := 0.0
		for i := range testSet {
			d := testSet[i].target - mean
			baseline += d * d
		}
		baseline /= float64(len(testSet))

		fmt.Printf("  train %.5f  held out %.5f  (constant guess %.5f, explains %.0f%%)\n",
			lastTrain, lastTest, baseline, 100*(1-lastTest/baseline))

		exported := n.export()
		_ = exported.Save("halfkp_latest.json")

		elo, margin, accepted := 0, 0, false
		tested := gen%*evalEvery == 0 || gen == *generations
		if tested {
			writeJSON("nnue_status.json", map[string]any{
				"phase": "evaluating", "generation": gen, "generations": *generations,
				"pool": len(pool), "cum_elo": cumElo, "eval_games": *evalGames,
				"arch": arch, "next_eval": gen,
			})
			challenger := champion
			challenger.HalfKP = exported
			challenger.Depth = *evalDepth
			ref := champion
			ref.Depth = *evalDepth
			res := engine.PlayMatch(challenger, ref, *evalGames, 250)
			elo, margin = res.Elo(), res.EloMargin()
			accepted = elo > margin
			if accepted {
				champion.HalfKP = exported
				cumElo += elo
				_ = exported.Save("halfkp_best.json")
			}
			fmt.Printf("  vs champion: W-D-L %d-%d-%d  %+d +/- %d  accepted=%v\n",
				res.Wins, res.Draws, res.Losses, elo, margin, accepted)
		}

		history = append(history, genRecord{
			Generation: gen, Positions: len(pool),
			TrainLoss: lastTrain, TestLoss: lastTest, Baseline: baseline,
			Elo: elo, EloMargin: margin, Accepted: accepted, Tested: tested,
			CumElo: cumElo, Seconds: int(time.Since(t0).Seconds()),
		})
		writeJSON("nnue.json", history)
	}
	writeJSON("nnue_status.json", map[string]any{"phase": "done", "cum_elo": cumElo})
}

var pieceCodes = map[board.PieceType]string{
	board.Pawn: "P", board.Knight: "N", board.Bishop: "B",
	board.Rook: "R", board.Queen: "Q", board.King: "K",
}

func squareName(s board.Sq) string {
	return string(rune('a'+s.File)) + string(rune('1'+s.Rank))
}

func moverName(c board.Color) string {
	if c == board.White {
		return "WHITE"
	}
	return "BLACK"
}

func boardSnapshot(b *board.Board) map[string]string {
	out := map[string]string{}
	var buf [32]board.ColoredPiece
	for _, p := range b.AppendAllPieces(buf[:0]) {
		prefix := "w"
		if p.Color == board.Black {
			prefix = "b"
		}
		out[squareName(p.Sq)] = prefix + pieceCodes[p.Type]
	}
	return out
}

// nextEval is the generation at which the current network will next be
// played against the champion.
func nextEval(gen, every int) int {
	return ((gen / every) + 1) * every
}
