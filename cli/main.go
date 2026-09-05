// Command chess-train runs Go self-play training, writing the same JSON
// files the existing browser UI (gui.html) already polls -- so the whole
// visualisation layer is reused unchanged against the Go backend.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"

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

func main() {
	generations := flag.Int("generations", 100, "number of generations")
	gamesPerGen := flag.Int("games", 6, "self-play games per candidate")
	population := flag.Int("population", 3, "candidate mutations per generation")
	depth := flag.Int("depth", 2, "search depth")
	maxMoves := flag.Int("max-moves", 100, "ply cap per game")
	benchGames := flag.Int("benchmark-games", 6, "games per benchmark")
	patience := flag.Int("patience", 5, "stop after this many generations without improvement")
	flag.Parse()

	genLabel := ""
	live := func(g *game.Game, plies int, from, to board.Sq) {
		writeJSON("live_game.json", map[string]any{
			"label":   genLabel,
			"move_no": plies,
			"mover":   moverName(g.Turn.Other()),
			"from":    squareName(from),
			"to":      squareName(to),
			"board":   boardSnapshot(&g.Board),
		})
	}

	history := []engine.GenerationRecord{}
	cfg := engine.Config{
		Generations:    *generations,
		GamesPerGen:    *gamesPerGen,
		PopulationSize: *population,
		Depth:          *depth,
		MaxMoves:       *maxMoves,
		BenchmarkGames: *benchGames,
		Patience:       *patience,
		ChampionPath:   "champion_weights.json",
		Live:           live,
		OnGeneration: func(r engine.GenerationRecord) {
			history = append(history, r)
			writeJSON("training_curve.json", history)
			writeJSON("training_status.json", map[string]any{
				"phase":           "playing",
				"generation":      r.Generation,
				"game":            len(r.Outcomes),
				"games_per_gen":   len(r.Outcomes),
				"running_score":   r.WinRate * float64(len(r.Outcomes)),
				"outcomes":        r.Outcomes,
				"population_size": 1,
				"candidate":       1,
			})
		},
	}
	genLabel = fmt.Sprintf("Go training, depth %d", *depth)

	champion, _ := engine.Train(cfg)
	fmt.Println("final champion:", champion)
	writeJSON("training_status.json", map[string]any{
		"phase": "done", "generation": *generations, "game": 0,
		"games_per_gen": *gamesPerGen, "running_score": 0, "outcomes": []string{},
	})
}

func moverName(c board.Color) string {
	if c == board.White {
		return "WHITE"
	}
	return "BLACK"
}
