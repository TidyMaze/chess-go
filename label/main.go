// Command label attaches a Stockfish evaluation to each position in the
// tuning set, so the evaluation can be fitted to a strong engine's
// judgement rather than to the result of a game between two weak ones.
//
// Tuning against self-play results was measured at -16 +/- 48 Elo. The
// fitted values showed the problem: the king piece-square table was
// scaled to zero, because in games between ~1900 engines it genuinely
// does not predict who wins. The outcome of one game is a single bit of
// very noisy information about a position; a depth-12 evaluation of that
// same position is a real number from a 3600-rated player.
//
// Positions are labelled in order and appended one line at a time, so the
// file itself is the resume marker: a re-run skips whatever is already
// done, and nothing paid for is held only in memory.
package main

import (
	"bufio"
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

const stockfishPath = "/opt/homebrew/bin/stockfish"

type sample struct {
	FEN    string  `json:"fen"`
	Result float64 `json:"result"`
}

type labelled struct {
	FEN    string  `json:"fen"`
	Result float64 `json:"result"`
	Score  float64 `json:"score"` // Stockfish, pawns, side to move
	Mate   bool    `json:"mate"`
}

func loadPositions(path string, limit int) []sample {
	f, err := os.Open(path)
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<22), 1<<22)
	var out []sample
	seen := map[string]bool{}
	for sc.Scan() {
		var batch []sample
		if json.Unmarshal(sc.Bytes(), &batch) != nil {
			continue
		}
		for _, s := range batch {
			// Duplicate positions would be paid for twice and would also
			// weight the fit toward whatever keeps recurring.
			if seen[s.FEN] {
				continue
			}
			seen[s.FEN] = true
			out = append(out, s)
			if limit > 0 && len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func countDone(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n := 0
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			n++
		}
	}
	return n
}

func main() {
	in := flag.String("in", "tuning_data.jsonl", "positions to label")
	out := flag.String("out", "tuning_labels.jsonl", "append-only labelled output")
	depth := flag.Int("depth", 12, "Stockfish search depth per position")
	limit := flag.Int("limit", 60000, "how many distinct positions to label")
	workers := flag.Int("workers", 8, "parallel Stockfish processes")
	static := flag.Bool("static", false, "label with Stockfish's static evaluation instead of a search")
	flag.Parse()

	positions := loadPositions(*in, *limit)
	done := countDone(*out)
	fmt.Printf("%d distinct positions, %d already labelled\n", len(positions), done)
	if done >= len(positions) {
		fmt.Println("nothing to do")
		return
	}
	todo := positions[done:]
	fmt.Printf("labelling %d at depth %d with %d Stockfish processes\n", len(todo), *depth, *workers)

	f, err := os.OpenFile(*out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("open out:", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)

	// Positions must be written in order for the line count to remain a
	// valid resume marker, so workers evaluate ahead in parallel and a
	// single writer drains the results in sequence.
	results := make([]chan labelled, len(todo))
	for i := range results {
		results[i] = make(chan labelled, 1)
	}

	next := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for wkr := 0; wkr < *workers; wkr++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sf, err := engine.NewStockfish(stockfishPath, 20, 0)
			if err != nil {
				return
			}
			defer sf.Close()
			for {
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= len(todo) {
					return
				}
				s := todo[i]
				rec := labelled{FEN: s.FEN, Result: s.Result}
				if g, err := game.ParseFEN(s.FEN); err == nil {
					if *static {
						// Static eval is already from White's point of view,
						// unlike the search score, so it is stored negated for
						// Black to keep one convention in the file.
						if score, ok := sf.StaticEval(g); ok {
							if g.Turn == board.Black {
								score = -score
							}
							rec.Score = score
						} else {
							rec.Mate = true
						}
					} else if score, mate, ok := sf.Evaluate(g, *depth); ok {
						rec.Score, rec.Mate = score, mate
					} else {
						rec.Mate = true // unusable; the fit skips these
					}
				}
				results[i] <- rec
			}
		}()
	}

	go func() { wg.Wait() }()

	t0 := time.Now()
	for i := range todo {
		rec := <-results[i]
		line, _ := json.Marshal(rec)
		w.Write(append(line, '\n'))
		if (i+1)%500 == 0 {
			w.Flush()
			f.Sync()
			rate := float64(i+1) / time.Since(t0).Seconds()
			fmt.Printf("  %d/%d, %.0f pos/s, eta %.0fs\n",
				i+1, len(todo), rate, float64(len(todo)-i-1)/rate)
		}
	}
	w.Flush()
	f.Sync()
	fmt.Printf("done: %d positions in %.0fs\n", len(todo), time.Since(t0).Seconds())
}
