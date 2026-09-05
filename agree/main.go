// Command agree measures how often this engine picks the move a strong
// Stockfish picks, at a range of search depths.
//
// Why this and not more games: game results at fixed depth were coming
// back inside their own error bars (+60 +/- 70 for depth 5 over 4, +38
// +/- 69 for depth 6 over 5), which cannot distinguish "one more ply is
// worth little" from "we did not play enough games". Agreement with a
// strong oracle needs no games at all, so thousands of positions cost
// minutes rather than hours, and it separates the two halves of the
// engine:
//
//   - if agreement climbs steadily with depth, the search is working and
//     the evaluation is what limits strength at any fixed depth;
//   - if agreement flattens, extra depth is not buying better moves, and
//     more search is the wrong place to spend effort.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"chess/engine"
	"chess/game"
)

const stockfishPath = "/opt/homebrew/bin/stockfish"

// collectPositions plays self-play games and keeps every position after
// the opening, so the sample looks like the positions the engine
// actually has to solve rather than a book of tactics.
func collectPositions(n, skipOpening int) []*game.Game {
	var out []*game.Game
	p := engine.Player{Depth: 3, UsePST: true, Quiescence: true, TTBits: 18,
		NullMove: true, Tapered: true, Iterative: true, Extensions: true,
		Aspiration: true, SEEPruning: true, Structure: true}
	for len(out) < n {
		g := game.New()
		g.EnableRepetitionTracking()
		for ply := 0; ply < 160 && len(out) < n; ply++ {
			if g.IsCheckmate(g.Turn) || g.IsStalemate(g.Turn) || g.KingCaptured {
				break
			}
			m, ok := engine.PlayerPick(p, g)
			if !ok {
				break
			}
			g.ApplyMove(m.From, m.To)
			if ply >= skipOpening {
				out = append(out, game.From(g.Board.Clone(), g.Turn))
			}
		}
	}
	return out
}

func main() {
	positions := flag.Int("positions", 300, "positions to test")
	seed := flag.Int64("seed", 1, "self-play seed, so every arm sees the same positions")
	oracleDepth := flag.Int("oracle-depth", 14, "Stockfish search depth")
	maxDepth := flag.Int("max-depth", 6, "highest depth to test for this engine")
	out := flag.String("out", "", "write results as JSON here")
	aspiration := flag.Bool("aspiration", true, "aspiration windows")
	nullMove := flag.Bool("nullmove", true, "null-move pruning")
	futility := flag.Bool("futility", true, "futility pruning")
	extensions := flag.Bool("extensions", true, "check extensions")
	iterative := flag.Bool("iterative", true, "iterative deepening search (false = legacy alpha-beta)")
	material := flag.Bool("material", false, "strip the evaluation down to material only")
	pst := flag.Bool("pst", true, "piece-square tables")
	useTT := flag.Bool("tt", true, "transposition table")
	flag.Parse()

	engine.SeedRandom(*seed)
	fmt.Printf("collecting %d positions from self-play...\n", *positions)
	games := collectPositions(*positions, 12)

	sf, err := engine.NewStockfish(stockfishPath, 20, 0)
	if err != nil {
		fmt.Println("stockfish error:", err)
		return
	}
	defer sf.Close()

	fmt.Printf("asking Stockfish (depth %d) for the best move in each...\n", *oracleDepth)
	best := make([]game.Move, len(games))
	haveBest := make([]bool, len(games))
	t0 := time.Now()
	for i, g := range games {
		best[i], haveBest[i] = sf.BestMove(g, *oracleDepth)
	}
	fmt.Printf("  done in %.0fs\n\n", time.Since(t0).Seconds())

	type row struct {
		Depth     int     `json:"depth"`
		Agreement float64 `json:"agreement"`
		Seconds   float64 `json:"seconds"`
	}
	var rows []row

	fmt.Printf("%-7s %-12s %-10s\n", "depth", "agreement", "time")
	for d := 1; d <= *maxDepth; d++ {
		ttBits := uint(20)
		if !*useTT {
			ttBits = 0
		}
		p := engine.Player{Depth: d, UsePST: *pst, Quiescence: true, TTBits: ttBits,
			MaterialOnly: *material,
			NullMove:     *nullMove, Tapered: true, Iterative: *iterative, Extensions: *extensions,
			Aspiration: *aspiration, SEEPruning: true, Structure: true, Futility: *futility}
		matched, total := 0, 0
		t := time.Now()
		for i, g := range games {
			if !haveBest[i] {
				continue
			}
			m, ok := engine.PlayerPick(p, game.From(g.Board.Clone(), g.Turn))
			if !ok {
				continue
			}
			total++
			if m.From == best[i].From && m.To == best[i].To {
				matched++
			}
		}
		secs := time.Since(t).Seconds()
		pct := 100 * float64(matched) / float64(total)
		rows = append(rows, row{d, pct, secs})
		fmt.Printf("%-7d %5.1f%% (%3d/%3d) %6.1fs\n", d, pct, matched, total, secs)
	}

	if *out != "" {
		data, _ := json.MarshalIndent(map[string]any{
			"oracle_depth": *oracleDepth, "positions": len(games), "rows": rows,
			"when": time.Now().Format(time.RFC3339),
		}, "", "  ")
		_ = os.WriteFile(*out, data, 0644)
	}
}
