// Command calibrate measures this engine against Stockfish at several of
// its own Elo-limited settings, and reports an Elo estimate on that
// externally-calibrated scale.
//
// Each Stockfish level gives an independent estimate (its rating plus the
// measured gap). Levels where the match is lopsided carry almost no
// information -- a 10-0 result only says "somewhere above" -- so the
// estimates are combined weighted by how close each match was to even.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"time"

	"chess/engine"
)

const stockfishPath = "/opt/homebrew/bin/stockfish"

type level struct {
	elo   int
	skill int
	depth int
}

func main() {
	games := flag.Int("games", 12, "games per Stockfish level")
	depth := flag.Int("depth", 5, "this engine's search depth")
	maxMoves := flag.Int("max-moves", 200, "ply cap")
	label := flag.String("label", "current", "name for this configuration")
	config := flag.String("config", "current", "which engine build: base | search | current | champion")
	champFile := flag.String("champion", "champion.json", "used when -config champion: the configuration to calibrate")
	out := flag.String("out", "", "append the result to this JSON file")
	workers := flag.Int("workers", runtime.NumCPU(), "games played in parallel")
	probe := flag.Int("probe", 8, "games per level in the first pass")
	flag.Parse()

	// Every milestone has to be measured on the same Stockfish scale for the
	// gains between them to mean anything, so the older builds stay
	// reachable here rather than only existing in git history.
	me := engine.Player{
		Name: *label, Depth: *depth, UsePST: true, Quiescence: true,
		TTBits: 20, NullMove: true, Tapered: true, Iterative: true,
	}
	if *config == "current" {
		me = engine.Strong(*depth)
		me.Name = *label
	}
	// "champion" calibrates whatever is actually being played, network
	// included. Without it this command could only measure the hand-written
	// evaluation, so the champion's rating was never anchored to anything:
	// it was the 1955 baseline plus a chain of relative deltas, each
	// measured against the previous champion rather than a fixed reference,
	// with the errors compounding and never checked.
	if *config == "champion" {
		c := engine.ReadChampion(*champFile)
		p, err := c.PlayerOrError()
		if err != nil {
			fmt.Println("champion:", err)
			return
		}
		me = p
		me.Depth = *depth
		me.Name = c.Label
		fmt.Printf("calibrating the champion: %s (claimed %.0f Elo)\n", c.Label, c.Elo)
	}

	switch *config {
	case "champion": // already built above
	case "base": // Go port as first completed: PST, quiescence, TT, null-move, tapered, ID.
	case "search": // + check extensions, aspiration windows, SEE pruning.
		me.Extensions, me.Aspiration, me.SEEPruning = true, true, true
	case "current": // + pawn structure and rook-file terms.
		me.Extensions, me.Aspiration, me.SEEPruning = true, true, true
		me.Structure = true
	default:
		fmt.Printf("unknown -config %q (want base, search or current)\n", *config)
		return
	}

	// The ladder has to reach above the engine or the result is a floor,
	// not a rating: the run after the transposition table fix scored 0.52
	// against the top level and had nothing above it to probe. Stockfish's
	// UCI_Elo runs to 3190. Skill Level is ignored once UCI_LimitStrength
	// is set, so those values only matter for the levels below 1600.
	levels := []level{
		{1600, 3, 4}, {1800, 5, 5}, {2000, 8, 6}, {2200, 11, 7}, {2400, 14, 8},
		{2600, 17, 9}, {2800, 19, 10}, {3000, 20, 11},
	}

	type estimate struct {
		level  level
		score  float64
		gap    int
		weight float64
	}
	var estimates []estimate

	// play runs one match against a level, in parallel across workers.
	play := func(lv level, n int) (engine.MatchResult, error) {
		return engine.PlayMatchAgainstUCI(me, func() (engine.Player, func(), error) {
			sf, err := engine.NewStockfish(stockfishPath, lv.skill, lv.elo)
			if err != nil {
				return engine.Player{}, func() {}, err
			}
			// Same clock on both sides when the champion plays under one.
			return engine.Player{Name: fmt.Sprintf("sf%d", lv.elo), UCI: sf, UCIDepth: lv.depth,
					UCIMoveTimeMS: int(me.TimeBudget / time.Millisecond)},
				func() { sf.Close() }, nil
		}, n, *maxMoves, *workers)
	}

	fmt.Printf("calibrating %q (depth %d) against Stockfish, %d workers\n", *label, *depth, *workers)
	whole := time.Now()

	// Pass 1: a short probe at every level. Most of a calibration run used
	// to be spent playing full matches against levels that turn out to be
	// far too strong or too weak, and those contribute almost nothing:
	// a 23-0 result only says "somewhere below". The probe finds which
	// levels are close before committing games to them.
	type probed struct {
		lv    level
		res   engine.MatchResult
		score float64
	}
	var first []probed
	for _, lv := range levels {
		res, err := play(lv, *probe)
		if err != nil {
			fmt.Println("stockfish error:", err)
			return
		}
		first = append(first, probed{lv, res, res.Score()})
		fmt.Printf("  probe vs SF %d: W-D-L %2d-%2d-%2d  score %.2f\n",
			lv.elo, res.Wins, res.Draws, res.Losses, res.Score())
	}

	// Pass 2: spend the remaining games on the levels nearest an even
	// match, where the result actually pins the rating down.
	sort.Slice(first, func(i, j int) bool {
		return math.Abs(first[i].score-0.5) < math.Abs(first[j].score-0.5)
	})
	focus := map[int]bool{}
	for i := 0; i < len(first) && i < 3; i++ {
		focus[first[i].lv.elo] = true
	}

	fmt.Println()
	for _, p := range first {
		lv, res := p.lv, p.res
		t0 := time.Now()
		if focus[lv.elo] && *games > *probe {
			extra, err := play(lv, *games-*probe)
			if err != nil {
				fmt.Println("stockfish error:", err)
				return
			}
			res.Wins += extra.Wins
			res.Draws += extra.Draws
			res.Losses += extra.Losses
		}
		score := res.Score()
		// Weight by closeness to an even match: a 50% result pins the
		// rating tightly, a 100% result barely constrains it at all.
		weight := 1 - 2*math.Abs(score-0.5)
		estimates = append(estimates, estimate{lv, score, res.Elo(), weight})
		mark := " "
		if focus[lv.elo] {
			mark = "*"
		}
		fmt.Printf(" %s vs SF %d (depth %d): W-D-L %2d-%2d-%2d  score %.2f  gap %+5d  -> %d Elo  [weight %.2f]  (%.0fs)\n",
			mark, lv.elo, lv.depth, res.Wins, res.Draws, res.Losses, score, res.Elo(),
			lv.elo+res.Elo(), weight, time.Since(t0).Seconds())
	}
	fmt.Printf("\n(* = levels the probe found closest to even, given the full game budget)\n")

	var num, den float64
	for _, e := range estimates {
		w := e.weight
		if w < 0.05 {
			w = 0.05 // never fully discard a level
		}
		num += w * float64(e.level.elo+e.gap)
		den += w
	}
	final := num / den
	fmt.Printf("\nCALIBRATED ELO (Stockfish scale): %.0f   (total %.0fs)\n", final, time.Since(whole).Seconds())

	if *out != "" {
		record := map[string]any{
			"label": *label, "depth": *depth, "elo": math.Round(final),
			"config": *config, "games_per_level": *games,
			"when": time.Now().Format(time.RFC3339),
		}
		var all []map[string]any
		if data, err := os.ReadFile(*out); err == nil {
			_ = json.Unmarshal(data, &all)
		}
		all = append(all, record)
		if data, err := json.MarshalIndent(all, "", "  "); err == nil {
			_ = os.WriteFile(*out, data, 0644)
		}
	}
}
