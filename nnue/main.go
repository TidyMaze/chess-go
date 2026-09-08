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
	"io"
	"math"
	"math/rand"
	"os"
	"runtime"
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
	target float64 // blended score in pawns, White's point of view
	static float64 // what the hand-written evaluation says about it
	game   int32
}

var writeMu sync.Mutex

// lastStatus is the most recent status map, refreshed by a heartbeat so
// that a long phase does not look like a stopped process.
//
// The evaluation phase plays 500 games and writes its status once, so the
// file sat untouched for three minutes while the trainer was busy and the
// staleness check reported it as not running. The phase durations here
// are minutes, so the file has to be touched on a timer rather than only
// when something changes.
var (
	lastStatusMu sync.Mutex
	lastStatus   map[string]any
)

func startHeartbeat(stop <-chan struct{}) {
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				lastStatusMu.Lock()
				m := lastStatus
				lastStatusMu.Unlock()
				if m != nil {
					writeJSON("nnue_status.json", m)
				}
			}
		}
	}()
}

func writeJSON(path string, v any) {
	// Status writes carry a timestamp so the browser can tell "this run
	// is between updates" from "this run is not running". Without it a
	// stopped trainer looks identical to a slow one, which is exactly how
	// a paused run gets read as a hung one.
	if m, ok := v.(map[string]any); ok {
		if _, has := m["phase"]; has {
			m["at"] = time.Now().Unix()
			lastStatusMu.Lock()
			lastStatus = m
			lastStatusMu.Unlock()
		}
	}
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
	// Adam's second-moment estimate, one per weight.
	//
	// Plain SGD with a single learning rate is a poor fit for a sparse
	// feature set: a common feature column receives hundreds of updates
	// per epoch and a rare one receives two, so any rate that moves the
	// rare ones destabilises the common ones. Adam scales each weight by
	// its own gradient history, which is exactly the mismatch here.
	//
	// The first moment (momentum) is deliberately omitted. Under Hogwild
	// the workers race, and momentum accumulates those races into a
	// persistent wrong direction; the second moment only ever grows, so
	// it degrades gracefully.
	v1   []float32
	vb1  []float32
	v2   []float32
	vb2  float32
	step int64
}

func newNet(h int, rng *rand.Rand) *net {
	inputs := engine.HalfKPInputsFor(engine.FeatureKingBuckets)
	n := &net{
		h:   h,
		w1:  make([]float32, inputs*h),
		b1:  make([]float32, h),
		w2:  make([]float32, 2*h),
		v1:  make([]float32, inputs*h),
		vb1: make([]float32, h),
		v2:  make([]float32, 2*h),
	}
	// About 30 features are active at once, so the accumulator is a sum
	// of 30 of these plus the bias, and the activation clips to [0,1].
	for i := range n.w1 {
		n.w1[i] = float32(rng.NormFloat64() * 0.02)
	}
	// The bias starts at 0.5, in the middle of the live range.
	//
	// Starting it at zero puts the accumulator around zero, which is the
	// bottom edge of the clip, so roughly half the units activate at
	// exactly 0 and receive exactly no gradient. They stay dead. With the
	// sigmoid in the loss the gradient is about twenty times smaller than
	// with a direct regression, too small to push them back into the live
	// range, and the network learned almost nothing: 11% of held-out
	// variance against 82% for the direct version.
	for i := range n.b1 {
		n.b1[i] = 0.5
	}
	// The output is a sum of 2h clipped activations, each in [0,1], times
	// these weights. To reach the +/-12 pawns the targets span, they have
	// to start large enough that the range is reachable at all.
	for i := range n.w2 {
		n.w2[i] = float32(rng.NormFloat64() * 0.4)
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

// loss is plain squared error against a target expressed in pawns.
//
// Three formulations were measured. Regressing directly on a probability
// target learned well (82% of held-out variance) but produced a network
// whose output is a probability, so a lost position evaluated as a small
// positive number and the pawn error in decided positions was enormous,
// because inverting a sigmoid amplifies: a 0.11 error at p=0.95 is four
// pawns. Putting the sigmoid in the loss instead, as Stockfish does, is
// correct in principle but its gradient is about twenty times smaller
// (k*p*(1-p) peaks at 0.075) and this optimiser could not follow it:
// 11% of variance at lr 0.015, and raising the rate destabilised Hogwild
// rather than helping.
//
// So the target is converted into pawns instead and regressed directly.
// The optimiser is the one that demonstrably works, and the output is in
// the units the search actually consumes.
func (n *net) loss(data []sample, k float64, useSigmoid bool) float64 {
	acc, act := make([]float32, 2*n.h), make([]float32, 2*n.h)
	total := 0.0
	for i := range data {
		out := float64(n.forward(&data[i], acc, act))
		if useSigmoid {
			out = sigmoid(out, k)
		}
		d := out - data[i].target
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
// trainProgress, when set, is called while an epoch runs.
//
// An epoch over ten million positions takes over three minutes at 256
// hidden units, and the status file was only written between epochs, so
// the browser showed "epoch 1 of 2" and nothing else for the whole time.
// A phase that reports nothing for three minutes is indistinguishable
// from a stalled one.
var trainProgress func(done, total int, rate float64, eta time.Duration)

func (n *net) trainEpoch(data []sample, order []int32, lr, decay float32, k float64, useSigmoid bool, workers int) {
	var wg sync.WaitGroup

	var done int64
	if trainProgress != nil {
		start := time.Now()
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					d := atomic.LoadInt64(&done)
					if d == 0 {
						continue
					}
					el := time.Since(start)
					rate := float64(d) / el.Seconds()
					eta := time.Duration(float64(len(order)-int(d))/rate) * time.Second
					trainProgress(int(d), len(order), rate, eta)
				}
			}
		}()
	}
	chunk := (len(order) + workers - 1) / workers
	h := n.h

	// Adam's second moment and the weight decay, both of which this
	// function used to accept and ignore.
	//
	// The state was allocated, checkpointed, round-trip tested and
	// documented at length while the loop ran plain SGD at a single rate,
	// so every sweep over -decay measured nothing. It matters here because
	// the feature set is sparse: a common column is updated hundreds of
	// times an epoch and a rare one twice, so any single rate that moves
	// the rare ones destabilises the common ones. Scaling each weight by
	// its own gradient history is exactly the mismatch to fix.
	//
	// The first moment is still deliberately absent. Under Hogwild the
	// workers race, and momentum accumulates those races into a persistent
	// wrong direction, while the second moment only grows and so degrades
	// gracefully.
	const beta2 = 0.999
	const eps = 1e-8
	step := atomic.AddInt64(&n.step, 1)
	// Bias correction. Without it the first updates divide by a
	// second moment still near zero and take enormous steps.
	corr := float32(1 / (1 - math.Pow(beta2, float64(step))))

	// adam applies one update in place. w and v are pointers into the
	// shared arrays; the races between workers are the point of Hogwild.
	adam := func(w, v *float32, g float32) {
		nv := beta2**v + (1-beta2)*g*g
		*v = nv
		*w -= lr * g / (float32(math.Sqrt(float64(nv*corr))) + eps)
	}

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
			since := 0
			for _, idx := range order[lo:hi] {
				// Counted in blocks: one atomic add per position would
				// contend across ten cores on a hot loop.
				if since++; since == 4096 {
					atomic.AddInt64(&done, int64(since))
					since = 0
				}
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
					adam(&n.w2[i], &n.v2[i], dOut*act[i])
				}
				adam(&n.b2, &n.vb2, dOut)

				for side, feats := range [2][]int32{s.own, s.opp} {
					off := side * h
					g := grad[off : off+h]
					for i := 0; i < h; i++ {
						adam(&n.b1[i], &n.vb1[i], g[i])
					}
					for _, f := range feats {
						col := int(f) * h
						wv := n.w1[col : col+h : col+h]
						vv := n.v1[col : col+h : col+h]
						for i := 0; i < h; i++ {
							adam(&wv[i], &vv[i], g[i])
							// Decay only the columns this position touched.
							// Decaying all 5120 columns every step would
							// shrink features the position says nothing
							// about, at 5120 times the cost.
							if decay > 0 {
								wv[i] -= lr * decay * wv[i]
							}
						}
					}
				}
			}
			atomic.AddInt64(&done, int64(since))
		}(lo, hi)
	}
	wg.Wait()
}

// export. The network's output is already an evaluation in pawns,
// because the sigmoid is applied in the loss rather than in the network,
// so no conversion is needed here.
// smooth blends each feature's weights toward those of the same piece on
// neighbouring squares.
//
// This attacks the measured blocker directly. The network is accurate but
// jumpy: it moves 0.58 pawns between positions one move apart against the
// hand-written evaluation's 0.38, and a blend sweep showed the damage is
// proportional to how much network is used. The jumpiness comes from
// adjacent squares learning independent weights, so a piece stepping one
// square swaps in a completely unrelated weight column.
//
// Chess evaluations are spatially smooth almost everywhere: a knight on
// e4 and one on e5 are worth nearly the same, and the exceptions (a pawn
// on the seventh rank) remain learnable, because this is a prior pulling
// weights together rather than a constraint forcing them equal.
//
// The same idea as a smoothness penalty in an image model, applied to the
// board instead of to pixels. One pass over 82k weights per epoch, which
// is nothing beside the epoch itself.
func (n *net) smooth(alpha float32) {
	if alpha <= 0 {
		return
	}
	h := n.h
	orig := append([]float32(nil), n.w1...)
	for feat := 0; feat < engine.HalfKPInputsFor(engine.FeatureKingBuckets); feat++ {
		sq := feat % 64
		base := feat - sq
		file, rank := sq%8, sq/8
		for i := 0; i < h; i++ {
			var sum float32
			count := 0
			for df := -1; df <= 1; df++ {
				for dr := -1; dr <= 1; dr++ {
					if df == 0 && dr == 0 {
						continue
					}
					f, r := file+df, rank+dr
					if f < 0 || f > 7 || r < 0 || r > 7 {
						continue
					}
					sum += orig[(base+r*8+f)*h+i]
					count++
				}
			}
			if count == 0 {
				continue
			}
			idx := feat*h + i
			n.w1[idx] = (1-alpha)*orig[idx] + alpha*(sum/float32(count))
		}
	}
}

func (n *net) export(k float64, sigmoidOut bool) *engine.HalfKPNet {
	return &engine.HalfKPNet{
		H:  n.h,
		W1: append([]float32(nil), n.w1...),
		B1: append([]float32(nil), n.b1...),
		W2: append([]float32(nil), n.w2...),
		B2: n.b2, Scale: 1, Sigmoid: sigmoidOut, K: k,
		Buckets: engine.FeatureKingBuckets,
	}
}

const (
	beta2 = float32(0.999)
	eps   = float32(1e-8)
)

// ---------------------------------------------------------------------
// data generation

// sigmoid maps a score in pawns to a win probability. K is the same
// constant the evaluation tuner fits.
func sigmoid(pawns, k float64) float64 { return 1 / (1 + math.Exp(-k*pawns)) }

// resultPawns expresses a game outcome on the same scale as a score.
//
// Stockfish blends the search score with the game result in probability
// space. Here the blend happens in pawns, so the outcome needs a pawn
// value: four pawns is roughly what a won game is worth as a statement
// about a quiet position, and it keeps the result term from dominating
// positions the search has already judged sharply.
func resultPawns(result float64) float64 { return (result - 0.5) * 8 }

// blendedTarget is lambda parts search score to one part game outcome,
// both in pawns and clamped to the range the network can represent.
func blendedTarget(score, result, lambda float64) float64 {
	t := lambda*score + (1-lambda)*resultPawns(result)
	if t > 12 {
		return 12
	}
	if t < -12 {
		return -12
	}
	return t
}

type genStats struct {
	games     int64
	positions int64
	truncated int64
}

// generate plays self-play games and returns labelled quiet positions.
//
// Labels are blended, as Stockfish does: the shallow search score carries
// the dense signal and the game result anchors it. Positions are kept
// only when nothing tactical is pending, since a static network cannot
// predict a capture sequence and training on those teaches noise.
func generate(champion engine.Player, games, playDepth, labelDepth, maxPlies int,
	lambda, k, quietTol float64, useSigmoidTarget bool, teachers chan *engine.UCIEngine, teacherDepth int,
	gen int, stats *genStats, openings []string,
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
			// Borrow a teacher from the shared pool for the duration of
			// this game. Creating one per game meant a process spawn and a
			// UCI handshake for every game, which dominated everything
			// else: the pool grew so slowly it looked like a deadlock.
			var teacher *engine.UCIEngine
			if teachers != nil {
				teacher = <-teachers
				defer func() { teachers <- teacher }()
			}
			// The playing search gets its own table for the whole game.
			// Allocating one per move was the single largest cost in the
			// generator: 24 MB a move, and the profile showed 58% of the
			// workload in the Go runtime because of it.
			playTT := engine.NewTranspositionTable(16)

			rng := rand.New(rand.NewSource(int64(gen)*1_000_003 + int64(gi)*7919))
			g := startingPosition(openings, rng)
			g.EnableRepetitionTracking()

			type pending struct {
				own, opp []int32
				score    float64
				static   float64
			}
			var kept []pending
			result := 0.5
			finished := false

			for ply := 0; ply < maxPlies; ply++ {
				if g.IsCheckmate(g.Turn) {
					if g.Turn == board.White {
						result = 0
					} else {
						result = 1
					}
					finished = true
					break
				}
				if g.IsStalemate(g.Turn) || g.IsFiftyMoveDraw() ||
					g.IsThreefoldRepetition() || g.KingCaptured {
					finished = true
					break
				}
				m, ok := engine.PlayerPickWith(player, g, playTT)
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
				if math.Abs(engine.QuiescenceScore(labeler, g)-staticSTM) > quietTol {
					continue
				}
				var score float64
				var scored bool
				if teacher != nil {
					var mate bool
					score, mate, scored = teacher.Evaluate(g, teacherDepth)
					if mate {
						// A forced mate saturates any target and says
						// nothing about the positional terms being learned.
						continue
					}
				} else {
					score, scored = engine.PlayerScoreWith(labeler, g, labelTT)
				}
				if !scored {
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
				kept = append(kept, pending{ownBuf, oppBuf, score, static})
			}

			// A game stopped by the ply cap did not end in a draw, it just
			// ran out of moves to play. Labelling it 0.5 tells the network
			// that a position one side was winning by five pawns was
			// balanced, and that lie is carried by every position in the
			// game. Truncated games are scored by what the search thinks
			// of the final position instead.
			if !finished {
				atomic.AddInt64(&stats.truncated, 1)
				if final, ok := engine.PlayerScoreWith(labeler, g, labelTT); ok {
					if g.Turn == board.Black {
						final = -final
					}
					result = sigmoid(final, 0.30)
				}
			}

			local := make([]sample, 0, len(kept))
			for _, p := range kept {
				tgt := blendedTarget(p.score, result, lambda)
				if useSigmoidTarget {
					// Win probability: the search score through a sigmoid,
					// blended with the game outcome, which is already a
					// probability (0, 0.5 or 1).
					tgt = lambda*sigmoid(p.score, k) + (1-lambda)*result
				}
				local = append(local, sample{
					own: p.own, opp: p.opp, game: int32(gi), static: p.static,
					target: tgt,
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
	HandMSE    float64 `json:"hand_mse"`
	Jump       float64 `json:"jump"`
	HandJump   float64 `json:"hand_jump"`
	GenSecs    int     `json:"gen_secs"`
	TrainSecs  int     `json:"train_secs"`
	EvalSecs   int     `json:"eval_secs"`
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
	// The network is tested blended with the hand-written evaluation,
	// because that is currently much the stronger way to use it: measured
	// over 800 games, 60% network scores -31 +/- 24 against the hand
	// evaluation while the network alone scores -118 +/- 36. Testing the
	// pure network would keep rejecting a combination that is close to
	// break-even. Set to 0 to test the network alone.
	evalBlend := flag.Float64("eval-blend", 0.4, "weight on the hand evaluation during the test match")
	epochs := flag.Int("epochs", 6, "training epochs per generation")
	// With Adam the update is lr * g / sqrt(v), which is order lr
	// regardless of gradient scale, so this is much smaller than the
	// plain-SGD rate it replaces.
	lr := flag.Float64("lr", 0.001, "learning rate")
	// L2 weight decay. With 40,960 x hidden parameters and far fewer
	// positions than that, the network memorises: generation 1 showed a
	// training loss of 0.0017 against a held-out 0.0473, a 28x gap.
	decay := flag.Float64("decay", 1e-4, "L2 weight decay on touched columns")
	smoothing := flag.Float64("smooth", 0.05, "pull each square's weights toward its neighbours")
	// "pawns" regresses directly on a pawn-valued target. "sigmoid" is
	// what Stockfish does: the network outputs a score, the loss applies
	// a sigmoid, and alpha-beta inverts it. The sigmoid version failed
	// once here (11% of variance) but that was under plain SGD, whose
	// single learning rate could not follow a gradient twenty times
	// smaller. Adam rescales per parameter, which is exactly that
	// problem, so it is worth retrying.
	target := flag.String("target", "pawns", "training target: pawns | sigmoid")
	// How close the quiescence score must be to the static score for a
	// position to count as quiet. Tighter means fewer positions but less
	// tactical noise in the target, which a static evaluation cannot
	// learn anyway.
	quietTol := flag.Float64("quiet", 0.35, "pawns of allowed static/quiescence disagreement")
	// Where the training labels come from.
	//
	// "self" is a search by this engine, which is the classic bootstrap
	// but caps the network near the strength of the engine producing the
	// labels: it is penalised whenever it discovers something a depth-5
	// search of an ~1850 player misses.
	//
	// "stockfish" distils from a much stronger search instead. Worth
	// distinguishing from the earlier failed experiments, which fitted a
	// *linear* model to Stockfish's *static* evaluation and removed the
	// terms a shallow search depends on. This is a non-linear network
	// learning search scores, which is what distillation normally means.
	teacher := flag.String("teacher", "self", "label source: self | stockfish")
	teacherDepth := flag.Int("teacher-depth", 10, "search depth for the teacher")
	poolFile := flag.String("pool-file", "nnue_pool.bin", "append-only positions, reloaded on restart")
	netFile := flag.String("net-file", "nnue_net.gob", "network and optimiser checkpoint")
	fresh := flag.Bool("fresh", false, "ignore any checkpoint and start over")
	freshPool := flag.Bool("fresh-pool", false, "discard stored positions too (fresh resets only the network)")
	lambda := flag.Float64("lambda", 0.8, "weight on the search score against the game result")
	k := flag.Float64("k", 0.30, "pawns-to-win-probability scale")
	hidden := flag.Int("hidden", 32, "hidden units per perspective")
	poolCap := flag.Int("pool", 3000000, "maximum positions kept")
	maxPlies := flag.Int("max-plies", 160, "ply cap in self-play")
	importPath := flag.String("import", "", "import the Lichess evaluation dump (JSONL, '-' for stdin) into the pool and exit")
	importMax := flag.Int("import-max", 0, "stop after this many imported positions (0 = the whole stream)")
	importQuiet := flag.Bool("import-quiet-filter", true, "keep only quiet positions when importing")
	importResume := flag.Bool("import-resume", true, "skip records already imported into this pool")
	extractOpenings := flag.String("extract-openings", "", "write an opening book of FENs from the dump named by -import and exit")
	extractBook := flag.String("extract-book", "", "write a playable opening book of FEN|move lines from the dump and exit")
	bookFromPGN := flag.String("book-from-pgn", "", "write an opening book of FEN|move lines from the moves played in the PGN named by -book-pgn, and exit")
	bookPGN := flag.String("book-pgn", "-", "PGN source for -book-from-pgn; - reads stdin")
	bookPlies := flag.Int("book-plies", 16, "record positions this many plies from the start")
	bookMin := flag.Int("book-min", 20, "a move must have been played in at least this many games")
	countPool := flag.String("count-pool", "", "print how many positions a pool file holds and exit")
	emitCheck := flag.String("emit-eval-check", "", "write FENs, their HalfKP features and this engine's evaluation of them, for cross-checking a PyTorch export")
	importPGNPath := flag.String("import-pgn", "", "import a PGN games file ('-' for stdin): moves and results only, labels computed here")
	pgnSkipPlies := flag.Int("pgn-skip-plies", 8, "opening plies to skip when importing games")
	labelChampion := flag.String("label-champion", "", "label positions with the champion described by this file, instead of the plain hand-written evaluation. This is what makes the bootstrap ladder climb.")
	extractTuning := flag.String("extract-tuning", "", "write the dump as Texel tuning records and exit")
	openingMinPieces := flag.Int("opening-min-pieces", 28, "pieces a position must still have to count as an opening")
	openingMax := flag.Int("opening-max", 200000, "how many opening positions to write")
	kingBuckets := flag.Int("king-buckets", 8, "king granularity in the feature set: 8 buckets or 32 canonical squares")
	openingBook := flag.String("opening-book", "", "start each self-play game from a position in this book instead of ten random plies")
	flag.Parse()

	if *bookFromPGN != "" {
		var src io.Reader = os.Stdin
		if *bookPGN != "-" {
			f, err := os.Open(*bookPGN)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			defer f.Close()
			src = f
		}
		out, err := os.Create(*bookFromPGN)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		n, err := BuildBookFromPGN(src, out, *bookPlies, *bookMin)
		out.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%s  wrote %d book positions to %s (plies <= %d, played in >= %d games)\n",
			time.Now().Format("15:04:05"), n, *bookFromPGN, *bookPlies, *bookMin)
		return
	}

	engine.FeatureKingBuckets = *kingBuckets

	// The opening book, if one was asked for. Loaded before anything else
	// so a missing or empty book is reported now rather than after a
	// generation of self-play has already started from the wrong place.
	var openings []string
	if *openingBook != "" {
		b, err := LoadOpenings(*openingBook)
		if err != nil {
			fmt.Println("opening book:", err)
			return
		}
		if len(b) == 0 {
			fmt.Printf("opening book %s is empty\n", *openingBook)
			return
		}
		openings = b
		fmt.Printf("opening book: %d positions from %s\n", len(openings), *openingBook)
	}

	// Importing is a separate job from training: it fills the pool from a
	// file rather than from self-play, and the training run that follows
	// reads that pool as usual.
	// Counting is exact where a file-size estimate is a guess: positions
	// average 96 bytes on real-game pools and 82 on self-play ones, so a
	// single constant misjudges completeness by a fifth, and a resumed
	// generation would call a pool finished at 80% of its target.
	if *countPool != "" {
		p, err := loadPool(*countPool, 0)
		if err != nil {
			fmt.Println("count-pool:", err)
			os.Exit(1)
		}
		fmt.Println(len(p))
		return
	}
	if *emitCheck != "" {
		if err := emitEvalCheck(*emitCheck, *netFile); err != nil {
			fmt.Println("emit-eval-check:", err)
		}
		return
	}
	if *importPGNPath != "" {
		src := os.Stdin
		if *importPGNPath != "-" {
			f, err := os.Open(*importPGNPath)
			if err != nil {
				fmt.Println("import-pgn:", err)
				return
			}
			defer f.Close()
			src = f
		}
		labeller := engine.Strong(*labelDepth)
		if *labelChampion != "" {
			c := engine.ReadChampion(*labelChampion)
			lp, err := c.PlayerOrError()
			if err != nil {
				// Refuse rather than degrade. Labelling with the hand
				// evaluation when the champion's network was meant to be
				// loaded produces a rung identical to the previous one,
				// and no number in the run would reveal it.
				fmt.Printf("%s  refusing to label: %v\n", time.Now().Format("15:04:05"), err)
				return
			}
			labeller = lp
			labeller.Depth = *labelDepth
			fmt.Printf("%s  labelling with the champion: %s\n",
				time.Now().Format("15:04:05"), c.Label)
		}
		if err := ImportPGN(src, *poolFile, *importMax, *labelDepth, *pgnSkipPlies,
			*lambda, *quietTol, 10*time.Second, *importResume, labeller); err != nil {
			fmt.Println("import-pgn:", err)
		}
		return
	}
	if *importPath != "" || *extractOpenings != "" || *extractBook != "" || *extractTuning != "" {
		src := os.Stdin
		if *importPath != "" && *importPath != "-" {
			f, err := os.Open(*importPath)
			if err != nil {
				fmt.Println("import:", err)
				return
			}
			defer f.Close()
			src = f
		}
		if *extractTuning != "" {
			if err := ExtractTuning(src, *extractTuning, *openingMax, *importQuiet, *quietTol); err != nil {
				fmt.Println("extract tuning:", err)
			}
			return
		}
		if *extractBook != "" {
			if err := ExtractBook(src, *extractBook, *openingMax, *openingMinPieces); err != nil {
				fmt.Println("extract book:", err)
			}
			return
		}
		if *extractOpenings != "" {
			if err := ExtractOpenings(src, *extractOpenings, *openingMax, *openingMinPieces); err != nil {
				fmt.Println("extract openings:", err)
			}
			return
		}
		if err := ImportLichess(src, *poolFile, *importMax, *quietTol, *importQuiet, *importResume); err != nil {
			fmt.Println("import:", err)
		}
		return
	}

	useSigmoid := *target == "sigmoid"
	// One Stockfish per core, started once and reused for the whole run.
	// A UCI process is a single conversation, so they cannot be shared
	// concurrently; a buffered channel hands each game exclusive use of
	// one and takes it back afterwards.
	var teachers chan *engine.UCIEngine
	if *teacher == "stockfish" {
		teachers = make(chan *engine.UCIEngine, runtime.NumCPU())
		for i := 0; i < runtime.NumCPU(); i++ {
			sf, err := engine.NewStockfish("/opt/homebrew/bin/stockfish", 20, 0)
			if err != nil {
				fmt.Println("stockfish:", err)
				return
			}
			defer sf.Close()
			teachers <- sf
		}
		fmt.Printf("teacher: Stockfish depth %d, %d processes\n", *teacherDepth, runtime.NumCPU())
	}
	rng := rand.New(rand.NewSource(23))
	n := newNet(*hidden, rng)

	// Resume rather than restart. A generation is a minute of ten cores,
	// so a pool of two million positions is over half an hour of compute,
	// and Adam's state is worth more still because it records how far
	// each weight has already been tuned. Both used to be discarded on
	// every restart.
	startGen, cumElo := 1, 0
	var pool []sample
	if !*fresh {
		if loaded, g, e, err := loadNet(*netFile, *hidden); err == nil {
			n, startGen, cumElo = loaded, g+1, e
			fmt.Printf("resumed from %s at generation %d (cumulative %+d Elo)\n", *netFile, g, e)
		} else if !os.IsNotExist(err) {
			fmt.Printf("not resuming: %v\n", err)
		}
	}
	// The pool is loaded whatever -fresh says. Positions are the expensive
	// half of this pipeline and they outlive any particular network: a
	// pool is hours of self-play or a 21 GB download, while a network is
	// minutes of fitting. Tying the two together meant that trying a new
	// architecture threw away the data it needed to be judged on.
	if !*freshPool {
		if p, err := loadPool(*poolFile, *poolCap); err == nil && len(p) > 0 {
			pool = p
			fmt.Printf("loaded %d positions from %s\n", len(pool), *poolFile)
		} else if err != nil && !os.IsNotExist(err) {
			fmt.Printf("pool %s: %v\n", *poolFile, err)
		}
	}
	workers := runtime.NumCPU()

	champion := engine.Strong(0)
	champion.Name = "champion"

	inputCount := engine.HalfKPInputsFor(engine.FeatureKingBuckets)
	params := inputCount*(*hidden) + *hidden + 2*(*hidden) + 1
	arch := map[string]any{
		"features":      fmt.Sprintf("HalfKP (%d king slots x piece x square)", engine.FeatureKingBuckets),
		"inputs":        inputCount,
		"hidden":        *hidden,
		"layers":        fmt.Sprintf("%d -> %d (shared, both perspectives) -> %d -> 1", inputCount, *hidden, 2**hidden),
		"activation":    "clipped ReLU [0,1]",
		"params":        params,
		"target":        fmt.Sprintf("%.2f x sigmoid(search score) + %.2f x game result", *lambda, 1-*lambda),
		"label_depth":   *labelDepth,
		"teacher":       *teacher,
		"teacher_depth": *teacherDepth,
		"play_depth":    *playDepth,
		"eval_every":    *evalEvery,
		"eval_games":    *evalGames,
		"eval_blend":    *evalBlend,
	}
	fmt.Printf("HalfKP %d inputs (%d king slots) x %d hidden per side, %d parameters, %d workers\n",
		inputCount, engine.FeatureKingBuckets, *hidden, params, workers)

	heartbeatStop := make(chan struct{})
	startHeartbeat(heartbeatStop)
	defer close(heartbeatStop)

	var history []genRecord
	if data, err := os.ReadFile("nnue.json"); err == nil && !*fresh {
		_ = json.Unmarshal(data, &history)
	}

	for gen := startGen; gen < startGen+*generations; gen++ {
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

		freshSamples := generate(champion, *gamesPerGen, *playDepth, *labelDepth, *maxPlies,
			*lambda, *k, *quietTol, useSigmoid, teachers, *teacherDepth, gen, stats, openings,
			func(g *game.Game, ply int, from, to board.Sq) {
				writeJSON("live_game.json", map[string]any{
					"label": fmt.Sprintf("Generation %d self-play", gen), "move_no": ply,
					"mover": moverName(g.Turn.Other()),
					"from":  squareName(from), "to": squareName(to),
					"board": boardSnapshot(&g.Board),
				})
			})
		close(done)

		// Appended before anything else touches it: the file is the
		// resume marker, so positions are on disk before the generation
		// that produced them can be lost.
		if err := appendPool(*poolFile, freshSamples); err != nil {
			fmt.Println("append pool:", err)
		}
		pool = append(pool, freshSamples...)
		if len(pool) > *poolCap {
			pool = pool[len(pool)-*poolCap:]
		}
		fmt.Printf("gen %d: %d new positions, pool %d, %d/%d games hit the ply cap (%.0fs)\n",
			gen, len(freshSamples), len(pool),
			atomic.LoadInt64(&stats.truncated), *gamesPerGen, time.Since(t0).Seconds())

		trainIdx, testSet := splitByGame(pool, rng, 15)
		if len(testSet) == 0 || len(trainIdx) == 0 {
			fmt.Printf("gen %d: pool holds %d positions, which cannot be split into a "+
				"training and a held-out set; nothing to train on\n", gen, len(pool))
			if len(pool) == 0 {
				fmt.Printf("the pool is empty: check -pool-file, and note that -fresh " +
					"resets the network only, not the positions\n")
				return
			}
			continue
		}

		genSecs := time.Since(t0).Seconds()
		trainStart := time.Now()
		var lastTrain, lastTest, handMSE, smoothness, handSmoothness float64
		// Carried into the per-epoch status so the browser can show the
		// share of variance explained, which is the number that says
		// whether the network is learning at all.
		lastExplained := 0.0
		for e := 1; e <= *epochs; e++ {
			rng.Shuffle(len(trainIdx), func(i, j int) {
				trainIdx[i], trainIdx[j] = trainIdx[j], trainIdx[i]
			})
			// Within-epoch progress. Without it the browser shows "epoch 1
			// of 2" and nothing else for three and a half minutes.
			epoch := e
			trainProgress = func(doneN, total int, rate float64, eta time.Duration) {
				writeJSON("nnue_status.json", map[string]any{
					"phase": "training", "generation": gen, "generations": *generations,
					"epoch": epoch, "epochs": *epochs, "test_loss": lastTest,
					"pool": len(pool), "cum_elo": cumElo,
					"arch": arch, "next_eval": nextEval(gen, *evalEvery),
					"epoch_positions": doneN, "epoch_total": total,
					"positions_per_sec": rate, "epoch_eta_sec": eta.Seconds(),
					"explains": lastExplained,
				})
			}
			n.trainEpoch(pool, trainIdx, float32(*lr), float32(*decay), *k, useSigmoid, workers)
			n.smooth(float32(*smoothing))
			lastTest = n.loss(testSet, *k, useSigmoid)
			if b := constantBaseline(testSet); b > 0 {
				lastExplained = 100 * (1 - lastTest/b)
			}
			writeJSON("nnue_status.json", map[string]any{
				"phase": "training", "generation": gen, "generations": *generations,
				"epoch": e, "epochs": *epochs, "test_loss": lastTest,
				"pool": len(pool), "cum_elo": cumElo, "explains": lastExplained,
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
		lastTrain = n.loss(sub, *k, useSigmoid)

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

		// The comparison that decides everything: how well does the
		// hand-written evaluation, which the network has to beat, predict
		// the same targets? A network that is worse than it here cannot
		// possibly be better than it in a game, and knowing that costs
		// nothing while a match costs minutes.
		handErr := 0.0
		for i := range testSet {
			d := testSet[i].static - testSet[i].target
			handErr += d * d
		}
		handErr /= float64(len(testSet))

		// Smoothness: how much the evaluation moves between positions one
		// move apart, which for consecutive samples from the same game is
		// exactly what they are.
		//
		// This is the quantity that decides whether a network can be used
		// at all, and it is not the same as accuracy. The search prunes on
		// pawn thresholds (aspiration window 0.5, futility 1 to 3), so an
		// evaluation that jumps a pawn per move makes all of them
		// misfire. Measured on the generation-12 network: 0.97 pawns per
		// move against the hand evaluation's 0.18, and a blend sweep gave
		// a clean dose-response (+9 Elo at 10% network, -80 at 30%, -207
		// at 50%): the more network, the worse, in proportion.
		netJump, handJump, pairs := 0.0, 0.0, 0
		accS, actS := make([]float32, 2*n.h), make([]float32, 2*n.h)
		for i := 1; i < len(testSet); i++ {
			if testSet[i].game != testSet[i-1].game {
				continue
			}
			a := float64(n.forward(&testSet[i-1], accS, actS))
			b := float64(n.forward(&testSet[i], accS, actS))
			netJump += math.Abs(b - a)
			handJump += math.Abs(testSet[i].static - testSet[i-1].static)
			pairs++
		}
		if pairs > 0 {
			netJump /= float64(pairs)
			handJump /= float64(pairs)
		}
		smoothness = netJump
		handSmoothness = handJump
		// The constant predictor is reported next to the losses, and the
		// verdict is taken against it rather than against the hand-written
		// evaluation.
		//
		// Beating the hand evaluation is not evidence of learning. On the
		// Lichess pool a network scored held-out 9.72 against the hand
		// evaluation's 13.5, printed "more accurate", and was worse than
		// predicting one constant for every position (9.80). The labels
		// had a sign bug and the network had learned 0.8% of the variance.
		// Nothing in the old line could show that.
		explained := 0.0
		if baseline > 0 {
			explained = 100 * (1 - lastTest/baseline)
		}
		verdict := "  <- learning nothing"
		switch {
		case explained >= 50:
			verdict = "  <- learning"
		case explained >= 10:
			verdict = "  <- learning slowly"
		}
		fmt.Printf("  train %.3f  held out %.3f  constant %.3f  explains %.1f%% (hand %.3f)  jump/move %.3f (hand %.3f)%s\n",
			lastTrain, lastTest, baseline, explained, handErr, smoothness, handSmoothness, verdict)
		handMSE = handErr
		_ = handSmoothness

		trainSecs := time.Since(trainStart).Seconds()
		evalStart := time.Now()

		exported := n.export(*k, useSigmoid)
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
			challenger.HalfKPBlend = *evalBlend
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

		evalSecs := time.Since(evalStart).Seconds()
		// Where the wall clock goes, which decides what is worth
		// optimising and specifically whether moving training to the GPU
		// would help at all.
		fmt.Printf("  time: %.0fs self-play, %.0fs training, %.0fs testing\n",
			genSecs, trainSecs, evalSecs)
		history = append(history, genRecord{
			Generation: gen, Positions: len(pool),
			TrainLoss: lastTrain, TestLoss: lastTest, Baseline: baseline, HandMSE: handMSE,
			Jump: smoothness, HandJump: handSmoothness,
			GenSecs: int(genSecs), TrainSecs: int(trainSecs), EvalSecs: int(evalSecs),
			Elo: elo, EloMargin: margin, Accepted: accepted, Tested: tested,
			CumElo: cumElo, Seconds: int(time.Since(t0).Seconds()),
		})
		writeJSON("nnue.json", history)
		if err := saveNet(*netFile, n, gen, cumElo); err != nil {
			fmt.Println("save net:", err)
		}
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
