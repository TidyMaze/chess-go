// Command trainloop is the self-play learning loop: the engine plays
// itself, learns to predict what its own deeper search finds, and the
// result is tested in games before it is kept.
//
// Why it learns from itself rather than from Stockfish. Four separate
// fits against Stockfish this session improved their objective and made
// the engine play worse, the clearest being one isolated term where a 14%
// better fit cost 82 Elo. Stockfish's evaluation is built to be corrected
// by a twenty-ply search, so it can afford to say almost nothing about
// things a five-ply search must be told. Fitting one to the other removes
// exactly what this engine depends on.
//
// So the target is this engine's own search, two plies deeper than the
// one that will use the network. That is the objective the evaluation
// actually needs to serve: a static score good enough that a shallow
// search reaches the same conclusion a deeper one would. It is the
// classic bootstrap (learn the deeper answer, then the deeper answer
// improves too), and it needs no external engine at all.
//
// Every generation writes its state to JSON so the browser can follow the
// run live: the network's loss, the measured Elo against the previous
// best, and one game in progress.
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
	"time"

	"chess/board"
	"chess/engine"
	"chess/game"
	"chess/moves"
)

const inputs = 768

// hidden is a variable because capacity turned out to be the binding
// constraint in the wrong direction: 256 units is 200,000 parameters,
// and a few hundred self-play games cannot support that. Held-out error
// (split by game) was 10.05 against the hand evaluation's 3.78.
var hidden = 256

type example struct {
	features []int32
	target   float64 // pawns, from White's point of view
	static   float64 // what the hand-written evaluation says about it
	game     int     // which self-play game it came from
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

func featureIndex(c board.Color, pt board.PieceType, sq board.Sq) int32 {
	return int32((int(c)*6+int(pt))*64 + int(sq.Rank)*8 + int(sq.File))
}

func featuresOf(b *board.Board) []int32 {
	var buf [32]board.ColoredPiece
	pieces := b.AppendAllPieces(buf[:0])
	out := make([]int32, 0, len(pieces))
	for _, p := range pieces {
		out = append(out, featureIndex(p.Color, p.Type, p.Sq))
	}
	return out
}

// ---------------------------------------------------------------------
// network

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
	for i := range n.w1 {
		n.w1[i] = float32(rng.NormFloat64() * 0.01)
	}
	for i := range n.w2 {
		n.w2[i] = float32(rng.NormFloat64() * 0.05)
	}
	return n
}

func (n *net) forward(feats []int32, acc, act []float32) float32 {
	copy(acc, n.b1)
	for _, f := range feats {
		col := int(f) * hidden
		w := n.w1[col : col+hidden : col+hidden]
		for i := 0; i < hidden; i++ {
			acc[i] += w[i]
		}
	}
	out := n.b2
	for i := 0; i < hidden; i++ {
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

func (n *net) loss(data []example) float64 {
	acc, act := make([]float32, hidden), make([]float32, hidden)
	total := 0.0
	for _, e := range data {
		d := float64(n.forward(e.features, acc, act)) - e.target
		total += d * d
	}
	return total / float64(len(data))
}

func (n *net) export(residual bool) *engine.Net {
	return &engine.Net{
		W1: append([]float32(nil), n.w1...),
		B1: append([]float32(nil), n.b1...),
		W2: append([]float32(nil), n.w2...),
		B2: n.b2, Scale: 1, Residual: residual,
	}
}

func (n *net) train(train, test []example, epochs int, lr float64, rng *rand.Rand,
	report func(epoch int, trainLoss, testLoss float64)) *net {

	acc, act := make([]float32, hidden), make([]float32, hidden)
	order := make([]int, len(train))
	for i := range order {
		order[i] = i
	}
	best, bestLoss := *n, math.Inf(1)

	for epoch := 1; epoch <= epochs; epoch++ {
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		eta := float32(lr)
		for _, idx := range order {
			e := train[idx]
			out := n.forward(e.features, acc, act)
			dOut := float32(2 * (float64(out) - e.target))
			for i := 0; i < hidden; i++ {
				grad := dOut * act[i]
				var dAct float32
				if acc[i] > 0 && acc[i] < 1 {
					dAct = dOut * n.w2[i]
				}
				n.w2[i] -= eta * grad
				act[i] = dAct
			}
			n.b2 -= eta * dOut
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
		tl, vl := n.loss(train), n.loss(test)
		if vl < bestLoss {
			// Keep the network with the lowest held-out loss, not the last
			// one: training past the best point is the default outcome, not
			// an unusual one.
			bestLoss = vl
			best = *n
			best.w1 = append([]float32(nil), n.w1...)
			best.b1 = append([]float32(nil), n.b1...)
			best.w2 = append([]float32(nil), n.w2...)
		}
		report(epoch, tl, vl)
	}
	return &best
}

// ---------------------------------------------------------------------
// self-play data

// collect plays games with the current best engine and labels every
// quiet position with that engine's own score at a deeper search.
//
// The label is what makes this self-learning rather than imitation: the
// deeper search is the same engine, just given more time, so the network
// is being asked to compress what the engine already knows into
// something it can see instantly at a leaf.
func collect(generation int, champion engine.Player, games, playDepth, labelDepth, maxPlies int, learnResidual bool,
	live func(*game.Game, int, board.Sq, board.Sq), progress func(done, positions int)) []example {

	var mu sync.Mutex
	var out []example
	done, positions := 0, 0

	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for gi := 0; gi < games; gi++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(gi int) {
			defer wg.Done()
			defer func() { <-sem }()

			gameID := generation*1000000 + gi
			player := champion
			player.Depth = playDepth
			labeler := champion
			labeler.Depth = labelDepth

			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(gi)*7919))
			g := game.New()
			g.EnableRepetitionTracking()
			// Random opening so the generations do not all learn the same
			// handful of positions.
			for i := 0; i < 8; i++ {
				legal := g.AllLegalMoves(g.Turn)
				if len(legal) == 0 {
					break
				}
				m := legal[rng.Intn(len(legal))]
				g.ApplyMove(m.From, m.To)
			}

			var local []example
			for ply := 0; ply < maxPlies && !g.IsOver(); ply++ {
				m, ok := engine.PlayerPick(player, g)
				if !ok {
					break
				}
				g.ApplyMove(m.From, m.To)
				if gi == 0 && live != nil {
					live(g, ply+1, m.From, m.To)
				}
				// Only quiet positions are worth learning from.
				//
				// The network is being asked to predict what a deeper
				// search concludes. Where a capture sequence is pending,
				// the difference between the static score and the search
				// score IS the tactic, and no function of piece placement
				// can predict it: that is what the search is for. Training
				// on those positions teaches noise, which is why the first
				// attempts left a held-out error near 1.8 pawns, easily
				// enough to hang a piece.
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

				// Deeper search of this position is the training target.
				score, ok := engine.PlayerScore(labeler, g)
				if !ok {
					continue
				}
				if g.Turn == board.Black {
					score = -score
				}
				if score > 8 {
					score = 8
				} else if score < -8 {
					score = -8
				}
				// The network is a residual: at play time its output is
				// added to the champion's own static evaluation. So the
				// target is what the static evaluation misses, not the
				// whole score. Training it on the whole score makes the
				// engine count the evaluation twice, which is what
				// generation 1 measured as -211 Elo.
				// Full score, not a correction to the static evaluation.
				//
				// Learning the residual on quiet positions is close to
				// degenerate: "quiet" means nothing tactical is pending,
				// which is precisely where the search score and the static
				// score already agree, so the residual is small and mostly
				// noise. Measured: held-out error sat at 1.32 while the
				// training error rose, meaning the network was fitting
				// noise and generalising nothing.
				//
				// Real NNUE learns the score itself on quiet positions,
				// which is a real function to learn.
				target := score
				if learnResidual {
					target = score - static
				}
				local = append(local, example{features: featuresOf(&g.Board), target: target, static: static, game: gameID})
			}

			mu.Lock()
			out = append(out, local...)
			done++
			positions += len(local)
			if progress != nil {
				progress(done, positions)
			}
			mu.Unlock()
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
	Elo        int     `json:"elo"`
	EloMargin  int     `json:"elo_margin"`
	Accepted   bool    `json:"accepted"`
	CumElo     int     `json:"cum_elo"`
	Seconds    int     `json:"seconds"`
}

func main() {
	generations := flag.Int("generations", 30, "training generations")
	gamesPerGen := flag.Int("games", 120, "self-play games per generation")
	playDepth := flag.Int("play-depth", 3, "search depth while generating games")
	labelDepth := flag.Int("label-depth", 5, "deeper search used as the training target")
	evalGames := flag.Int("eval-games", 300, "games to test each new network")
	evalDepth := flag.Int("eval-depth", 4, "depth for the test match")
	epochs := flag.Int("epochs", 12, "training epochs per generation")
	lr := flag.Float64("lr", 0.02, "learning rate")
	maxPlies := flag.Int("max-plies", 200, "ply cap in self-play")
	residual := flag.Bool("residual", false, "learn a correction to the hand evaluation instead of replacing it")
	hiddenFlag := flag.Int("hidden", 256, "hidden units")
	flag.Parse()

	hidden = *hiddenFlag
	rng := rand.New(rand.NewSource(11))
	n := newNet(rng)

	// The champion is the engine that generates data and that each new
	// network must beat to be adopted. It starts as the hand-written
	// evaluation and is replaced whenever a network wins its match.
	champion := engine.Player{
		Name: "champion", UsePST: true, Quiescence: true, TTBits: 20,
		NullMove: true, Tapered: true, Iterative: true, Extensions: true,
		Aspiration: true, SEEPruning: true, Structure: true, Futility: true,
	}

	var history []genRecord
	cumElo := 0
	pool := make([]example, 0, 400000)

	for gen := 1; gen <= *generations; gen++ {
		t0 := time.Now()
		writeJSON("train_status.json", map[string]any{
			"phase": "self-play", "generation": gen, "generations": *generations,
			"games": 0, "games_total": *gamesPerGen, "positions": 0,
			"cum_elo": cumElo,
		})

		fresh := collect(gen, champion, *gamesPerGen, *playDepth, *labelDepth, *maxPlies, *residual,
			func(g *game.Game, ply int, from, to board.Sq) {
				writeJSON("live_game.json", map[string]any{
					"label":   fmt.Sprintf("Generation %d self-play", gen),
					"move_no": ply,
					"mover":   moverName(g.Turn.Other()),
					"from":    squareName(from),
					"to":      squareName(to),
					"board":   boardSnapshot(&g.Board),
				})
			},
			func(done, positions int) {
				writeJSON("train_status.json", map[string]any{
					"phase": "self-play", "generation": gen, "generations": *generations,
					"games": done, "games_total": *gamesPerGen, "positions": positions,
					"cum_elo": cumElo,
				})
			})

		// Keep a sliding window of recent generations. Old positions came
		// from a weaker champion and its labels are worse, but throwing
		// everything away each generation makes the network forget.
		pool = append(pool, fresh...)
		if len(pool) > 400000 {
			pool = pool[len(pool)-400000:]
		}
		fmt.Printf("gen %d: %d new positions, pool %d\n", gen, len(fresh), len(pool))

		// Split by game, not by position.
		//
		// Splitting positions at random leaks badly: consecutive
		// positions in one game differ by a single move, so a position in
		// the test set has near-copies of itself in the training set and
		// the network can score well by memorising its neighbours. That
		// produced a held-out error of 1.36 against the hand evaluation's
		// 5.89, apparently a 4x better evaluation, while the same network
		// scored 2.32 against the hand evaluation's 2.12 on positions from
		// games it had never seen, and lost every game it played.
		//
		// Holding out whole games removes the near-copies.
		data := append([]example(nil), pool...)
		gameIDs := map[int]bool{}
		for _, e := range data {
			gameIDs[e.game] = true
		}
		ids := make([]int, 0, len(gameIDs))
		for id := range gameIDs {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
		heldOut := map[int]bool{}
		for _, id := range ids[:1+len(ids)*15/100] {
			heldOut[id] = true
		}
		var trainSet, testSet []example
		for _, e := range data {
			if heldOut[e.game] {
				testSet = append(testSet, e)
			} else {
				trainSet = append(trainSet, e)
			}
		}
		if len(testSet) == 0 || len(trainSet) == 0 {
			fmt.Println("not enough games to split; skipping generation")
			continue
		}

		writeJSON("train_status.json", map[string]any{
			"phase": "training", "generation": gen, "generations": *generations,
			"positions": len(pool), "cum_elo": cumElo,
		})

		var lastTrain, lastTest float64
		n = n.train(trainSet, testSet, *epochs, *lr, rng,
			func(epoch int, tl, vl float64) {
				lastTrain, lastTest = tl, vl
				writeJSON("train_status.json", map[string]any{
					"phase": "training", "generation": gen, "generations": *generations,
					"epoch": epoch, "epochs": *epochs, "train_loss": tl, "test_loss": vl,
					"positions": len(pool), "cum_elo": cumElo,
				})
			})
		// Variance of the held-out target: what a network that always
		// guessed the mean would score. A held-out loss near this means
		// the network has learned nothing, which is not visible from the
		// loss alone.
		mean := 0.0
		for _, e := range testSet {
			mean += e.target
		}
		mean /= float64(len(testSet))
		variance := 0.0
		for _, e := range testSet {
			variance += (e.target - mean) * (e.target - mean)
		}
		variance /= float64(len(testSet))
		explained := 100 * (1 - lastTest/variance)

		// The comparison that matters: how well does the hand-written
		// evaluation, which the network is trying to beat, predict the
		// same targets? A network that is more accurate and still loses
		// games is not failing on accuracy, and knowing that stops the
		// next twenty generations being spent on more data.
		handErr := 0.0
		for _, e := range testSet {
			d := e.static - e.target
			handErr += d * d
		}
		handErr /= float64(len(testSet))
		fmt.Printf("  loss train %.4f held out %.4f (variance %.4f, explains %.0f%%); hand eval %.4f\n",
			lastTrain, lastTest, variance, explained, handErr)

		// The only test that counts: does it win games against the current
		// champion? Every fit this session that was judged by its loss
		// alone turned out to play worse.
		writeJSON("train_status.json", map[string]any{
			"phase": "evaluating", "generation": gen, "generations": *generations,
			"positions": len(pool), "cum_elo": cumElo,
			"eval_games": *evalGames,
		})
		challenger := champion
		challenger.Net = n.export(*residual)
		// Saved whether or not it is adopted: a rejected network is the
		// thing that most needs inspecting.
		_ = challenger.Net.Save("net_latest.json")
		challenger.Depth = *evalDepth
		ref := champion
		ref.Depth = *evalDepth
		res := engine.PlayMatch(challenger, ref, *evalGames, 250)
		elo, margin := res.Elo(), res.EloMargin()

		// Adopt only on evidence, not on a positive point estimate: a
		// result inside its own margin is a coin flip, and adopting those
		// is how a loop drifts sideways for twenty generations.
		accepted := elo > margin
		if accepted {
			champion.Net = challenger.Net
			cumElo += elo
			_ = challenger.Net.Save(fmt.Sprintf("net_gen%d.json", gen))
			_ = challenger.Net.Save("net_best.json")
		}
		fmt.Printf("  vs champion: W-D-L %d-%d-%d  %+d +/- %d  accepted=%v\n",
			res.Wins, res.Draws, res.Losses, elo, margin, accepted)

		history = append(history, genRecord{
			Generation: gen, Positions: len(pool),
			TrainLoss: lastTrain, TestLoss: lastTest,
			Elo: elo, EloMargin: margin, Accepted: accepted,
			CumElo: cumElo, Seconds: int(time.Since(t0).Seconds()),
		})
		writeJSON("training.json", history)
		writeJSON("train_status.json", map[string]any{
			"phase": "done-generation", "generation": gen, "generations": *generations,
			"positions": len(pool), "cum_elo": cumElo,
			"last_elo": elo, "last_margin": margin, "accepted": accepted,
		})
	}

	writeJSON("train_status.json", map[string]any{"phase": "done", "cum_elo": cumElo})
	fmt.Printf("done, cumulative measured gain %+d Elo\n", cumElo)
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
