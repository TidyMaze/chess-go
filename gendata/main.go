// Command gendata produces the training set for Texel tuning: positions
// from self-play, each labelled with the result of the game it came from.
//
// The expensive part of tuning is generating these games, not fitting to
// them, so games are appended to a JSONL file the moment they finish and
// a re-run skips whatever is already on disk. The paid work is never held
// only in memory.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"chess/board"
	"chess/engine"
	"chess/game"
	"chess/moves"
)

// sample is one labelled position. Result is from White's point of view:
// 1 White won, 0.5 draw, 0 Black won.
type sample struct {
	FEN    string  `json:"fen"`
	Result float64 `json:"result"`
}

// playGame plays one self-play game and returns its quiet positions.
//
// Openings are randomised, otherwise every game would start the same way
// and the set would be thousands of copies of one opening. Positions
// where the side to move is in check are dropped: Texel tuning fits a
// static evaluation to game outcomes, and a static score is meaningless
// where the move is forced and the position is about to change sharply.
func playGame(depth, randomPlies, maxPlies int) []sample {
	p := engine.Player{Depth: depth, UsePST: true, Quiescence: true, TTBits: 18,
		NullMove: true, Tapered: true, Iterative: true, Extensions: true,
		Aspiration: true, SEEPruning: true, Structure: true, Futility: true}
	rnd := engine.Player{Random: true}

	g := game.New()
	g.EnableRepetitionTracking()
	var fens []string
	result := 0.5

	for ply := 0; ply < maxPlies; ply++ {
		if g.IsCheckmate(g.Turn) {
			// The side to move is mated, so the other side won.
			if g.Turn == board.White {
				result = 0
			} else {
				result = 1
			}
			break
		}
		if g.IsStalemate(g.Turn) || g.IsFiftyMoveDraw() || g.IsThreefoldRepetition() || g.KingCaptured {
			result = 0.5
			break
		}
		picker := p
		if ply < randomPlies {
			picker = rnd
		}
		m, ok := engine.PlayerPick(picker, g)
		if !ok {
			break
		}
		g.ApplyMove(m.From, m.To)
		if ply >= randomPlies && !moves.IsInCheck(&g.Board, g.Turn) {
			fens = append(fens, g.FEN())
		}
	}

	out := make([]sample, len(fens))
	for i, f := range fens {
		out[i] = sample{FEN: f, Result: result}
	}
	return out
}

// countGames reports how many games are already recorded, so a re-run
// tops the file up instead of starting over.
func countGames(path string) (games int, positions int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var batch []sample
		if json.Unmarshal(sc.Bytes(), &batch) == nil {
			games++
			positions += len(batch)
		}
	}
	return games, positions
}

func main() {
	games := flag.Int("games", 500, "total games wanted in the file")
	depth := flag.Int("depth", 3, "search depth for self-play")
	randomPlies := flag.Int("random-plies", 8, "opening plies played at random, for variety")
	maxPlies := flag.Int("max-plies", 240, "ply cap per game")
	out := flag.String("out", "tuning_data.jsonl", "append-only JSONL output")
	flag.Parse()

	have, havePos := countGames(*out)
	fmt.Printf("%s already has %d games (%d positions)\n", *out, have, havePos)
	if have >= *games {
		fmt.Println("nothing to do")
		return
	}
	todo := *games - have
	fmt.Printf("playing %d more at depth %d on %d cores\n", todo, *depth, runtime.NumCPU())

	f, err := os.OpenFile(*out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("open:", err)
		return
	}
	defer f.Close()

	// One line per game, written as soon as that game finishes: the file
	// is the resume marker, so a crash costs at most the games in flight.
	var writeMu sync.Mutex
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	var done, positions int
	t0 := time.Now()

	for i := 0; i < todo; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			batch := playGame(*depth, *randomPlies, *maxPlies)
			if len(batch) == 0 {
				return
			}
			line, err := json.Marshal(batch)
			if err != nil {
				return
			}
			writeMu.Lock()
			defer writeMu.Unlock()
			f.Write(append(line, '\n'))
			f.Sync()
			done++
			positions += len(batch)
			if done%25 == 0 {
				rate := float64(done) / time.Since(t0).Seconds()
				fmt.Printf("  %d/%d games, %d positions, %.1f games/s, eta %.0fs\n",
					done, todo, positions, rate, float64(todo-done)/rate)
			}
		}()
	}
	wg.Wait()
	fmt.Printf("done: %d games, %d positions in %.0fs\n", done, positions, time.Since(t0).Seconds())
}
