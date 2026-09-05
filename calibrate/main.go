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
	config := flag.String("config", "current", "which engine build: base | search | current")
	out := flag.String("out", "", "append the result to this JSON file")
	flag.Parse()

	// Every milestone has to be measured on the same Stockfish scale for the
	// gains between them to mean anything, so the older builds stay
	// reachable here rather than only existing in git history.
	me := engine.Player{
		Name: *label, Depth: *depth, UsePST: true, Quiescence: true,
		TTBits: 20, NullMove: true, Tapered: true, Iterative: true,
	}
	switch *config {
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

	levels := []level{
		{1600, 3, 4}, {1800, 5, 5}, {2000, 8, 6}, {2200, 11, 7}, {2400, 14, 8},
	}

	type estimate struct {
		level  level
		score  float64
		gap    int
		weight float64
	}
	var estimates []estimate

	fmt.Printf("calibrating %q (depth %d) against Stockfish\n", *label, *depth)
	for _, lv := range levels {
		sf, err := engine.NewStockfish(stockfishPath, lv.skill, lv.elo)
		if err != nil {
			fmt.Println("stockfish error:", err)
			return
		}
		opp := engine.Player{Name: fmt.Sprintf("sf%d", lv.elo), UCI: sf, UCIDepth: lv.depth}
		t0 := time.Now()
		res := engine.PlayMatchSerial(me, opp, *games, *maxMoves)
		sf.Close()

		score := res.Score()
		// Weight by closeness to an even match: a 50% result pins the
		// rating tightly, a 100% result barely constrains it at all.
		weight := 1 - 2*math.Abs(score-0.5)
		estimates = append(estimates, estimate{lv, score, res.Elo(), weight})
		fmt.Printf("  vs SF %d (depth %d): W-D-L %2d-%2d-%2d  score %.2f  gap %+5d  -> %d Elo  [weight %.2f]  (%.0fs)\n",
			lv.elo, lv.depth, res.Wins, res.Draws, res.Losses, score, res.Elo(), lv.elo+res.Elo(), weight, time.Since(t0).Seconds())
	}

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
	fmt.Printf("\nCALIBRATED ELO (Stockfish scale): %.0f\n", final)

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
