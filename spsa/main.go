// Command spsa tunes evaluation parameters against game results.
//
// Every other tuning attempt in this project fitted the evaluation to
// something that could be predicted: self-play outcomes, Stockfish's
// search score, Stockfish's static score. All three improved their
// objective and made the engine play worse, the sharpest case being one
// isolated term where a 14% better fit cost 82 Elo. Squared prediction
// error and playing strength are different objectives and they diverge.
//
// SPSA (Spall, 1992) optimises the objective that matters. It needs no
// gradient and no model of the function: perturb every parameter at once
// by a random sign, play the two perturbed engines against each other,
// and step in whichever direction won. Two matches per iteration
// regardless of how many parameters there are, which is why it is what
// chess engines actually use for this.
//
// The cost is that the signal is game results, which are noisy. A match
// of 200 games resolves about +/- 48 Elo, so a single iteration cannot
// tell a good step from a bad one. SPSA does not need it to: it needs the
// steps to be right slightly more often than chance, and averages over
// iterations.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

	"chess/engine"
)

// knob is one tunable number, with the bounds it must stay inside and the
// scale of a sensible perturbation.
type knob struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Step  float64 `json:"step"` // size of a one-unit perturbation
}

// The tunable set is the five new terms whose hand-picked weights
// measured -11 +/- 17 stacked. The terms describe real features; being
// wrong about their size is a different failure from their being
// useless, and SPSA optimises the objective that matters rather than a
// proxy.
func defaultKnobs() []knob {
	sh := engine.DefaultShapeWeights()
	return []knob{
		{"outpost", sh.Outpost, 0, 0.50, 0.025},
		{"connected", sh.Connected, 0, 0.30, 0.015},
		{"backward", sh.Backward, 0, 0.40, 0.020},
		{"badBishop", sh.BadBishop, 0, 0.20, 0.010},
		{"passed", 0.06, 0, 0.40, 0.020},
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// player builds an engine from a parameter vector.
func player(ks []knob, depth int) engine.Player {
	byName := map[string]float64{}
	for _, k := range ks {
		byName[k.Name] = k.Value
	}
	sh := engine.ShapeWeights{
		Outpost: byName["outpost"], Connected: byName["connected"],
		Backward: byName["backward"], BadBishop: byName["badBishop"],
	}
	str := engine.DefaultStructureWeights()
	str.PassedBase = byName["passed"]
	str.PassedPerRank = byName["passed"] / 2

	p := engine.Strong(depth)
	p.Name = "spsa"
	p.Shape, p.ShapeW = true, &sh
	p.StructureW = &str
	return p
}

type iterRecord struct {
	Iteration int       `json:"iteration"`
	Score     float64   `json:"score"` // plus-side score in its match
	Elo       int       `json:"elo"`
	Values    []float64 `json:"values"`
	Seconds   int       `json:"seconds"`
}

func main() {
	iterations := flag.Int("iterations", 60, "SPSA iterations")
	games := flag.Int("games", 160, "games per iteration")
	depth := flag.Int("depth", 4, "search depth")
	// a is calibrated so that a clearly-won iteration moves a parameter by
	// a meaningful fraction of its step. The first version used 0.35 and
	// combined it with a division by the perturbation size, which made the
	// updates about 7e-5 against step sizes of 0.012: the parameters did
	// not move at all over five iterations.
	a := flag.Float64("a", 6.0, "step size gain")
	c := flag.Float64("c", 1.0, "perturbation gain, in units of each knob's step")
	seed := flag.Int64("seed", 17, "random seed")
	checkGames := flag.Int("check-games", 800, "games in the final check against the starting values")
	out := flag.String("out", "spsa.json", "progress output for the UI")
	state := flag.String("state", "spsa_state.json", "checkpoint, so a restart resumes")
	fresh := flag.Bool("fresh", false, "ignore the checkpoint and start over")
	flag.Parse()

	rng := rand.New(rand.NewSource(*seed))
	ks := defaultKnobs()
	start := append([]knob(nil), ks...)

	// Resume rather than restart. Each iteration is a 200-game match, so
	// a run that has reached iteration 30 represents an hour of ten
	// cores, and the parameter vector it has walked to is the entire
	// product of that hour.
	startIter := 1
	var history []iterRecord
	if !*fresh {
		if data, err := os.ReadFile(*state); err == nil {
			var st struct {
				Iteration int          `json:"iteration"`
				Values    []float64    `json:"values"`
				History   []iterRecord `json:"history"`
			}
			if json.Unmarshal(data, &st) == nil && len(st.Values) == len(ks) {
				for i := range ks {
					ks[i].Value = st.Values[i]
				}
				startIter = st.Iteration + 1
				history = st.History
				fmt.Printf("resumed from %s at iteration %d\n", *state, st.Iteration)
			}
		}
	}

	fmt.Printf("SPSA over %d parameters, %d iterations of %d games at depth %d\n",
		len(ks), *iterations, *games, *depth)
	for _, k := range ks {
		fmt.Printf("  %-14s %.4f  [%.2f, %.2f]\n", k.Name, k.Value, k.Min, k.Max)
	}

	t0 := time.Now()

	for iter := startIter; iter < startIter+*iterations; iter++ {
		it0 := time.Now()
		// Gains decay so early iterations explore and later ones settle.
		ak := *a / math.Pow(float64(iter)+10, 0.602)
		ck := *c / math.Pow(float64(iter), 0.101)

		// One random sign per parameter: the whole vector moves at once,
		// which is what makes the cost independent of dimension.
		delta := make([]float64, len(ks))
		plus := append([]knob(nil), ks...)
		minus := append([]knob(nil), ks...)
		for i := range ks {
			if rng.Intn(2) == 0 {
				delta[i] = -1
			} else {
				delta[i] = 1
			}
			d := delta[i] * ck * ks[i].Step
			plus[i].Value = clamp(ks[i].Value+d, ks[i].Min, ks[i].Max)
			minus[i].Value = clamp(ks[i].Value-d, ks[i].Min, ks[i].Max)
		}

		res := engine.PlayMatch(player(plus, *depth), player(minus, *depth), *games, 250)
		score := res.Score() // how the plus side did, 0..1

		// Gradient estimate. Only the sign and rough size matter: with
		// 160 games the score is noisy, and SPSA converges as long as the
		// steps are right more often than not.
		grad := 2 * (score - 0.5)
		for i := range ks {
			ks[i].Value = clamp(ks[i].Value+ak*grad*delta[i]*ks[i].Step,
				ks[i].Min, ks[i].Max)
		}

		values := make([]float64, len(ks))
		for i := range ks {
			values[i] = ks[i].Value
		}
		history = append(history, iterRecord{
			Iteration: iter, Score: score, Elo: res.Elo(),
			Values: values, Seconds: int(time.Since(it0).Seconds()),
		})
		if data, err := json.Marshal(map[string]any{
			"names": names(ks), "history": history,
			"iterations": *iterations, "elapsed": int(time.Since(t0).Seconds()),
		}); err == nil {
			_ = os.WriteFile(*out, data, 0644)
		}
		// Checkpoint after every iteration: the state is small and the
		// work behind it is a whole match.
		if data, err := json.Marshal(map[string]any{
			"iteration": iter, "values": values, "history": history,
		}); err == nil {
			tmp := *state + ".tmp"
			if os.WriteFile(tmp, data, 0644) == nil {
				_ = os.Rename(tmp, *state)
			}
		}
		fmt.Printf("iter %3d: plus scored %.3f (%+d Elo)  %s  (%.0fs)\n",
			iter, score, res.Elo(), summary(ks), time.Since(it0).Seconds())
	}

	fmt.Println("\nfinal values:")
	for i, k := range ks {
		fmt.Printf("  %-14s %.4f  (was %.4f)\n", k.Name, k.Value, start[i].Value)
	}

	// The only result that counts: the tuned vector against the values it
	// started from, in a match large enough to mean something.
	fmt.Printf("\nchecking tuned vs starting values over %d games...\n", *checkGames)
	res := engine.PlayMatch(player(ks, *depth), player(start, *depth), *checkGames, 250)
	fmt.Printf("  W-D-L %d-%d-%d  score %.3f\n", res.Wins, res.Draws, res.Losses, res.Score())
	fmt.Printf("  Elo %+d +/- %d\n", res.Elo(), res.EloMargin())
	if res.Elo() > res.EloMargin() {
		fmt.Println("  CONFIRMED: the tuned values are stronger.")
	} else {
		fmt.Println("  not confirmed: inside its own margin, so not adopted.")
	}
}

func names(ks []knob) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = k.Name
	}
	return out
}

func summary(ks []knob) string {
	s := ""
	for _, k := range ks {
		s += fmt.Sprintf("%.3f ", k.Value)
	}
	return s
}
