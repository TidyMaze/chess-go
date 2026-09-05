// Command arena runs engine-vs-engine matches on this machine and streams
// the results as JSON for the browser UI: a live board of one game from
// the current match, the running match record, and the fitted Elo table.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"chess/board"
	"chess/engine"
	"chess/game"
)

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

var pieceCodes = map[board.PieceType]string{
	board.Pawn: "P", board.Knight: "N", board.Bishop: "B",
	board.Rook: "R", board.Queen: "Q", board.King: "K",
}

func squareName(s board.Sq) string {
	return string(rune('a'+s.File)) + string(rune('1'+s.Rank))
}

func boardSnapshot(b *board.Board) map[string]string {
	out := map[string]string{}
	for _, color := range []board.Color{board.White, board.Black} {
		prefix := "w"
		if color == board.Black {
			prefix = "b"
		}
		for _, ps := range b.PiecesOf(color) {
			out[squareName(ps.Sq)] = prefix + pieceCodes[ps.Type]
		}
	}
	return out
}

func moverName(c board.Color) string {
	if c == board.White {
		return "WHITE"
	}
	return "BLACK"
}

type ratingRow struct {
	Name string  `json:"name"`
	Elo  float64 `json:"elo"`
}

func main() {
	gamesPerPair := flag.Int("games", 12, "games per pair")
	maxMoves := flag.Int("max-moves", 250, "ply cap per game")
	flag.Parse()

	strong := func(name string, d int) engine.Player {
		return engine.Player{Name: name, Depth: d, UsePST: true, Quiescence: true,
			TTBits: 20, NullMove: true, Tapered: true, Iterative: true}
	}
	players := []engine.Player{
		{Name: "random", Random: true},
		{Name: "greedy captures", Greedy: true},
		{Name: "depth 1", Depth: 1},
		{Name: "depth 2 (BASELINE)", Depth: 2},
		{Name: "depth 2 +PST +quiescence", Depth: 2, UsePST: true, Quiescence: true},
		strong("depth 3 FULL", 3),
		strong("depth 4 FULL", 4),
		strong("depth 5 FULL", 5),
	}

	// Sequential round-robin so the UI can follow one match at a time;
	// games within each match still run in parallel across all cores.
	type pair struct{ i, j int }
	var pairs []pair
	for i := range players {
		for j := i + 1; j < len(players); j++ {
			pairs = append(pairs, pair{i, j})
		}
	}

	results := make([]engine.PairScore, 0, len(pairs))
	completed := []map[string]any{}

	for n, pr := range pairs {
		a, b := players[pr.i], players[pr.j]
		label := fmt.Sprintf("%s  vs  %s", a.Name, b.Name)
		writeJSON("arena_status.json", map[string]any{
			"phase": "playing", "match": n + 1, "total_matches": len(pairs),
			"label": label, "games_per_pair": *gamesPerPair,
		})

		start := time.Now()
		res := engine.PlayMatchLive(a, b, *gamesPerPair, *maxMoves,
			func(g *game.Game, plies int, from, to board.Sq) {
				writeJSON("live_game.json", map[string]any{
					"label":   label,
					"move_no": plies,
					"mover":   moverName(g.Turn.Other()),
					"from":    squareName(from),
					"to":      squareName(to),
					"board":   boardSnapshot(&g.Board),
				})
			})

		results = append(results, engine.PairScore{I: pr.i, J: pr.j, Score: res.Score(), Games: res.Games()})
		completed = append(completed, map[string]any{
			"a": a.Name, "b": b.Name,
			"wins": res.Wins, "draws": res.Draws, "losses": res.Losses,
			"elo_gap": res.Elo(), "seconds": int(time.Since(start).Seconds()),
		})
		writeJSON("arena_matches.json", completed)

		// Refit ratings after every match so the table fills in live.
		ratings := engine.FitFromResults(len(players), results, 0)
		rows := make([]ratingRow, len(players))
		for i, p := range players {
			rows[i] = ratingRow{Name: p.Name, Elo: ratings[i]}
		}
		writeJSON("arena_ratings.json", rows)
		fmt.Printf("[%2d/%2d] %-52s W-D-L %2d-%2d-%2d  (%ds)\n",
			n+1, len(pairs), label, res.Wins, res.Draws, res.Losses, int(time.Since(start).Seconds()))
	}

	writeJSON("arena_status.json", map[string]any{"phase": "done", "total_matches": len(pairs)})
	fmt.Println("arena complete")
}
