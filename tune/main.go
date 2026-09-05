// Command tune fits the evaluation's parameters to game outcomes, the
// method Peter Osterlund described for Texel in 2014.
//
// The idea: a static evaluation is trying to predict who wins. So take
// hundreds of thousands of positions from real games, map each static
// score through a sigmoid to get a predicted win probability, and choose
// the parameters that minimise the squared error against what actually
// happened. No games are played during fitting, which is what makes it
// affordable: the genetic algorithm this replaces needed a match to
// score every candidate and could only afford to move five numbers.
//
// Why this and not more search: measured on 2026-09-05, agreement with a
// depth-14 Stockfish stops improving past depth 4 (60.4% at depth 4,
// 61.4% at depth 6 for six times the time), and with a material-only
// evaluation it falls instead of plateauing. The engine already searches
// deeper than its evaluation can tell positions apart.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sync"
	"time"

	"chess/board"
	"chess/engine"
	"chess/game"
)

type sample struct {
	FEN    string  `json:"fen"`
	Result float64 `json:"result"`
}

type position struct {
	board board.Board
	// target is what the evaluation is being fitted to, already mapped
	// into win-probability space: either the game's result, or Stockfish's
	// evaluation of this position passed through the same sigmoid.
	target float64
}

// params is everything being fitted, flattened so a coordinate descent
// can walk it without caring what each number means.
//
// The pawn is deliberately not tuned. Every evaluation term is expressed
// relative to a pawn, so if the pawn moves too then the whole vector can
// drift to a scaled copy of itself with an identical error, and the fit
// has a free direction it can wander along forever.
type params struct {
	material  [6]float64
	pstScale  [6]float64
	structure engine.StructureWeights
	mobility  [6]float64
}

type knob struct {
	name string
	get  func(*params) float64
	set  func(*params, float64)
	min  float64
	max  float64
	step float64
}

func knobs() []knob {
	pieceKnob := func(name string, pt board.PieceType, lo, hi, step float64) knob {
		return knob{name, func(p *params) float64 { return p.material[pt] },
			func(p *params, v float64) { p.material[pt] = v }, lo, hi, step}
	}
	mobKnob := func(name string, pt board.PieceType) knob {
		return knob{name, func(p *params) float64 { return p.mobility[pt] },
			func(p *params, v float64) { p.mobility[pt] = v }, 0, 0.3, 0.005}
	}
	scaleKnob := func(name string, pt board.PieceType) knob {
		return knob{name, func(p *params) float64 { return p.pstScale[pt] },
			func(p *params, v float64) { p.pstScale[pt] = v }, 0, 4, 0.1}
	}
	return []knob{
		pieceKnob("knight", board.Knight, 1.5, 5, 0.05),
		pieceKnob("bishop", board.Bishop, 1.5, 5, 0.05),
		pieceKnob("rook", board.Rook, 3, 8, 0.05),
		pieceKnob("queen", board.Queen, 6, 14, 0.1),
		scaleKnob("pst:pawn", board.Pawn),
		scaleKnob("pst:knight", board.Knight),
		scaleKnob("pst:bishop", board.Bishop),
		scaleKnob("pst:rook", board.Rook),
		scaleKnob("pst:queen", board.Queen),
		scaleKnob("pst:king", board.King),
		{"isolated", func(p *params) float64 { return p.structure.Isolated },
			func(p *params, v float64) { p.structure.Isolated = v }, 0, 1, 0.02},
		{"doubled", func(p *params) float64 { return p.structure.Doubled },
			func(p *params, v float64) { p.structure.Doubled = v }, 0, 1, 0.02},
		{"rookOpen", func(p *params) float64 { return p.structure.RookOpen },
			func(p *params, v float64) { p.structure.RookOpen = v }, 0, 1, 0.02},
		{"rookSemiOpen", func(p *params) float64 { return p.structure.RookSemiOpen },
			func(p *params, v float64) { p.structure.RookSemiOpen = v }, 0, 1, 0.02},
		{"passedBase", func(p *params) float64 { return p.structure.PassedBase },
			func(p *params, v float64) { p.structure.PassedBase = v }, 0, 1, 0.02},
		{"passedPerRank", func(p *params) float64 { return p.structure.PassedPerRank },
			func(p *params, v float64) { p.structure.PassedPerRank = v }, 0, 0.5, 0.01},
		{"kingShield", func(p *params) float64 { return p.structure.KingShield },
			func(p *params, v float64) { p.structure.KingShield = v }, 0, 0.5, 0.01},
		mobKnob("mob:knight", board.Knight),
		mobKnob("mob:bishop", board.Bishop),
		mobKnob("mob:rook", board.Rook),
		mobKnob("mob:queen", board.Queen),
	}
}

// apply builds the Eval for a candidate parameter vector. Everything
// travels on the Eval, so evaluating two candidates concurrently is safe.
func apply(p *params) *engine.Eval {
	w, scale, sw, mob := p.material, p.pstScale, p.structure, p.mobility
	return &engine.Eval{Weights: &w, UsePST: true, Tapered: true, Structure: true,
		PSTScale: &scale, StructureW: &sw, Mobility: true, MobilityW: &mob}
}

// meanSquaredError is the quantity being minimised: how far the sigmoid
// of the static score sits from the game's actual result.
func meanSquaredError(positions []position, ev *engine.Eval, k float64) float64 {
	workers := runtime.NumCPU()
	chunk := (len(positions) + workers - 1) / workers
	sums := make([]float64, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		lo := w * chunk
		hi := lo + chunk
		if lo >= len(positions) {
			break
		}
		if hi > len(positions) {
			hi = len(positions)
		}
		wg.Add(1)
		go func(w, lo, hi int) {
			defer wg.Done()
			total := 0.0
			for _, p := range positions[lo:hi] {
				b := p.board
				score := engine.PositionScoreEval(&b, board.White, ev)
				predicted := 1 / (1 + math.Exp(-k*score))
				d := p.target - predicted
				total += d * d
			}
			sums[w] = total
		}(w, lo, hi)
	}
	wg.Wait()
	total := 0.0
	for _, s := range sums {
		total += s
	}
	return total / float64(len(positions))
}

// bestK scales the evaluation onto the probability curve. Without it the
// error is measuring the arbitrary units of the evaluation as much as its
// quality, and every parameter would be pushed toward whatever magnitude
// happens to suit a fixed curve.
func bestK(positions []position, ev *engine.Eval) (float64, float64) {
	bestErr, best := math.Inf(1), 0.4
	for k := 0.05; k <= 2.0; k += 0.05 {
		if e := meanSquaredError(positions, ev, k); e < bestErr {
			bestErr, best = e, k
		}
	}
	return best, bestErr
}

// loadLabelled reads positions carrying a Stockfish evaluation and turns
// that evaluation into the fitting target.
//
// Stockfish reports from the side to move; this engine's score is from
// White. Positions where Stockfish found a forced mate are dropped: the
// target would saturate the sigmoid and say nothing about the positional
// terms being fitted, which is exactly what the search is for.
func loadLabelled(path string, limit int, k float64) []position {
	f, err := os.Open(path)
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<22), 1<<22)
	var out []position
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
		score := rec.Score
		if g.Turn == board.Black {
			score = -score
		}
		out = append(out, position{board: g.Board, target: 1 / (1 + math.Exp(-k*score))})
		if limit > 0 && len(out) >= limit {
			return out
		}
	}
	return out
}

func load(path string, limit int) []position {
	f, err := os.Open(path)
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<22), 1<<22)
	var out []position
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var batch []sample
		if json.Unmarshal(sc.Bytes(), &batch) != nil {
			continue
		}
		for _, s := range batch {
			g, err := game.ParseFEN(s.FEN)
			if err != nil {
				continue
			}
			out = append(out, position{board: g.Board, target: s.Result})
			if limit > 0 && len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func main() {
	data := flag.String("data", "tuning_data.jsonl", "labelled positions")
	limit := flag.Int("limit", 0, "cap positions loaded (0 = all)")
	passes := flag.Int("passes", 8, "coordinate descent passes")
	holdout := flag.Float64("holdout", 0.2, "fraction held back to check for overfitting")
	out := flag.String("out", "tuned_params.json", "where to write the fitted parameters")
	seed := flag.Int64("seed", 7, "shuffle seed for the train/held-out split")
	labels := flag.String("labels", "", "fit to Stockfish scores in this file instead of game results")
	fixedK := flag.Float64("k", 0.30, "sigmoid scale used with -labels")
	flag.Parse()

	t0 := time.Now()
	var all []position
	fitK := true
	if *labels != "" {
		all = loadLabelled(*labels, *limit, *fixedK)
		// K is what maps pawns onto win probability. With Stockfish scores
		// as the target both sides of the comparison must go through the
		// same K, otherwise the fit can lower the error by rescaling the
		// evaluation rather than improving it.
		fitK = false
	} else {
		all = load(*data, *limit)
	}
	if len(all) < 1000 {
		fmt.Printf("only %d positions loaded; generate more with gendata\n", len(all))
		return
	}
	// Shuffle before splitting. Positions arrive in game order, so an
	// unshuffled tail is the last few hundred games rather than a sample
	// of all of them: the first run of this showed the held-out error
	// starting *below* the training error, which is not something a fair
	// split does and meant the two slices were not comparable.
	rng := rand.New(rand.NewSource(*seed))
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })

	// A held-out slice the fit never sees. Coordinate descent will happily
	// drive the training error down by memorising quirks of this
	// particular set of self-play games; if the held-out error stops
	// following, that is what is happening.
	split := int(float64(len(all)) * (1 - *holdout))
	train, test := all[:split], all[split:]
	fmt.Printf("loaded %d positions (%d train, %d held out) in %.0fs\n",
		len(all), len(train), len(test), time.Since(t0).Seconds())

	start := &params{
		material:  *engine.DefaultWeights(),
		pstScale:  engine.DefaultPSTScale(),
		structure: engine.DefaultStructureWeights(),
		mobility:  engine.DefaultMobilityWeights(),
	}
	cur := *start

	ev := apply(&cur)
	var k, trainErr float64
	if fitK {
		k, trainErr = bestK(train, ev)
	} else {
		k = *fixedK
		trainErr = meanSquaredError(train, ev, k)
	}
	testErr := meanSquaredError(test, ev, k)
	fmt.Printf("K = %.2f, starting error: train %.6f, held out %.6f\n\n", k, trainErr, testErr)
	startTrain, startTest := trainErr, testErr

	ks := knobs()
	for pass := 1; pass <= *passes; pass++ {
		improved := false
		for _, kn := range ks {
			// Once a direction helps, keep going in it rather than moving
			// one step per pass: a scale that wants to be 0.4 should not
			// need six passes to get there.
			for _, dir := range []float64{1, -1} {
				moved := false
				for {
					v := kn.get(&cur) + dir*kn.step
					if v < kn.min || v > kn.max {
						break
					}
					trial := cur
					kn.set(&trial, v)
					e := meanSquaredError(train, apply(&trial), k)
					if e >= trainErr-1e-9 {
						break
					}
					trainErr, cur = e, trial
					improved, moved = true, true
				}
				if moved {
					break // this direction worked; the opposite one will not
				}
			}
		}
		ev = apply(&cur)
		testErr = meanSquaredError(test, ev, k)
		fmt.Printf("pass %d: train %.6f  held out %.6f\n", pass, trainErr, testErr)
		if !improved {
			fmt.Println("no parameter moved; stopping")
			break
		}
	}

	fmt.Printf("\nerror: train %.6f -> %.6f (%.2f%%), held out %.6f -> %.6f (%.2f%%)\n",
		startTrain, trainErr, 100*(startTrain-trainErr)/startTrain,
		startTest, testErr, 100*(startTest-testErr)/startTest)

	fmt.Println("\nparameter            before     after")
	for _, kn := range ks {
		b, a := kn.get(start), kn.get(&cur)
		flag := ""
		if math.Abs(a-b) > 1e-9 {
			flag = "  <-"
		}
		fmt.Printf("  %-18s %7.3f  %7.3f%s\n", kn.name, b, a, flag)
	}

	record := map[string]any{
		"k": k, "positions": len(all),
		"train_error_before": startTrain, "train_error_after": trainErr,
		"test_error_before": startTest, "test_error_after": testErr,
		"material": cur.material, "pst_scale": cur.pstScale, "mobility": cur.mobility,
		"structure": map[string]float64{
			"PassedBase": cur.structure.PassedBase, "PassedPerRank": cur.structure.PassedPerRank,
			"Isolated": cur.structure.Isolated, "Doubled": cur.structure.Doubled,
			"RookOpen": cur.structure.RookOpen, "RookSemiOpen": cur.structure.RookSemiOpen,
			"KingShield": cur.structure.KingShield,
		},
		"when": time.Now().Format(time.RFC3339),
	}
	if data, err := json.MarshalIndent(record, "", "  "); err == nil {
		_ = os.WriteFile(*out, data, 0644)
		fmt.Printf("\nwrote %s\n", *out)
	}
}
