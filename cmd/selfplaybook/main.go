// Command selfplaybook generates an opening book purely from local self-play games
// using the Champion AI, ensuring every entry represents the engine's own best move.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"chess/engine"
	"chess/game"
)

var defaultSeedOpenings = []string{
	// Standard Openings
	"e2e4",
	"d2d4",
	"c2c4",
	"g1f3",
	"f2f4",
	"b2b3",
	"g2g3",
	"b1c3",
	// Open Game (1. e4 e5)
	"e2e4 e7e5",
	"e2e4 e7e5 g1f3 b8c6",
	"e2e4 e7e5 g1f3 b8c6 f1b5", // Ruy Lopez
	"e2e4 e7e5 g1f3 b8c6 f1c4", // Italian
	"e2e4 e7e5 g1f3 g8f6",       // Petrov
	"e2e4 e7e5 f2f4",             // King's Gambit
	// Semi-Open Games
	"e2e4 c7c5",             // Sicilian
	"e2e4 c7c5 g1f3 d7d6",    // Classical / Najdorf Sicilian
	"e2e4 c7c5 g1f3 b8c6",    // Open Sicilian
	"e2e4 c7c5 g1f3 e7e6",    // French Sicilian
	"e2e4 e7e6",             // French Defense
	"e2e4 e7e6 d2d4 d7d5",    // French Mainline
	"e2e4 c7c6",             // Caro-Kann
	"e2e4 c7c6 d2d4 d7d5",    // Caro-Kann Mainline
	"e2e4 d7d6",             // Pirc
	"e2e4 d7d5",             // Scandinavian
	"e2e4 g7g6",             // Modern
	"e2e4 g8f6",             // Alekhine
	// Closed Games (1. d4)
	"d2d4 d7d5",
	"d2d4 d7d5 c2c4",       // Queen's Gambit
	"d2d4 d7d5 c2c4 e7e6", // QGD
	"d2d4 d7d5 c2c4 c7c6", // Slav
	"d2d4 d7d5 g1f3",       // London / Colle
	"d2d4 g8f6",             // Indian Defenses
	"d2d4 g8f6 c2c4 g7g6", // King's Indian / Grunfeld
	"d2d4 g8f6 c2c4 e7e6", // Nimzo / Queen's Indian
	"d2d4 g8f6 c2c4 c7c5", // Benoni
	"d2d4 f7f5",             // Dutch
	// Flank Openings
	"c2c4 e7e5", // English
	"c2c4 c7c5", // Symmetrical English
	"c2c4 g8f6",
	"g1f3 d7d5", // Reti
	"g1f3 g8f6",
}

func main() {
	champFile := flag.String("champion", "champion.json", "champion config file")
	outPath := flag.String("out", "champion_selfplay_book.txt", "output opening book file")
	numGames := flag.Int("games", 300, "number of self-play games")
	maxPlies := flag.Int("plies", 16, "maximum opening plies per game to record")
	depth := flag.Int("depth", 6, "search depth per move")
	workers := flag.Int("workers", runtime.NumCPU(), "concurrent self-play workers")
	flag.Parse()

	champ := engine.ReadChampion(*champFile)
	basePlayer, err := champ.PlayerOrError()
	if err != nil {
		fmt.Printf("Warning: failed to load champion player (%v), falling back to Strong\n", err)
		basePlayer = engine.Strong(*depth)
	}
	basePlayer.Book = nil // Must search on its own, not use an old book
	basePlayer.Depth = *depth
	basePlayer.Threads = 1

	fmt.Printf("=== Building Self-Play Opening Book ===\n")
	fmt.Printf("Champion: %s\n", champ.Label)
	fmt.Printf("Config: %d games, max %d plies, search depth %d, across %d workers\n",
		*numGames, *maxPlies, *depth, *workers)
	fmt.Printf("Output: %s\n\n", *outPath)

	var bookMu sync.Mutex
	bookMap := make(map[string]string) // FEN key -> best move (UCI)

	// Build game seeds: cycle through default openings plus variations
	seeds := make([]string, *numGames)
	for i := 0; i < *numGames; i++ {
		seeds[i] = defaultSeedOpenings[i%len(defaultSeedOpenings)]
	}

	gameChan := make(chan int, *numGames)
	for i := 0; i < *numGames; i++ {
		gameChan <- i
	}
	close(gameChan)

	var gamesDone int64
	var movesRecorded int64
	startTime := time.Now()

	// Progress reporter goroutine
	stopProgress := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopProgress:
				return
			case <-ticker.C:
				done := atomic.LoadInt64(&gamesDone)
				if done == 0 {
					continue
				}
				elapsed := time.Since(startTime)
				pct := float64(done) / float64(*numGames) * 100.0
				rate := float64(done) / elapsed.Seconds()
				eta := time.Duration(float64(*numGames-int(done))/rate) * time.Second

				bookMu.Lock()
				uniquePos := len(bookMap)
				keys := make([]string, 0, uniquePos)
				for k := range bookMap {
					keys = append(keys, k)
				}
				bookMu.Unlock()
				sort.Strings(keys)

				// Atomically flush current book to disk
				tmpFile := *outPath + ".tmp"
				if f, err := os.Create(tmpFile); err == nil {
					bookMu.Lock()
					for _, k := range keys {
						fmt.Fprintf(f, "%s 0 1|%s\n", k, bookMap[k])
					}
					bookMu.Unlock()
					f.Close()
					_ = os.Rename(tmpFile, *outPath)
				}

				fmt.Printf("  [%3.0f%%] Games: %d/%d | Unique Positions: %d | Rate: %.1f games/s | Elapsed: %s | ETA: %s\n",
					pct, done, *numGames, uniquePos, rate,
					elapsed.Truncate(time.Second), eta.Truncate(time.Second))
			}
		}
	}()

	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			p := basePlayer
			tt := engine.NewTranspositionTable(18) // Local 256K entry TT per worker

			for gameIdx := range gameChan {
				g := game.New()
				seedMoves := strings.Fields(seeds[gameIdx])

				// Play seed moves onto board
				for _, mvStr := range seedMoves {
					m, ok := game.MoveFromUCI(mvStr)
					if !ok {
						break
					}
					g.ApplyMove(m.From, m.To)
				}

				// Now self-play using Champion search up to maxPlies
				for ply := len(seedMoves); ply < *maxPlies; ply++ {
					if g.IsOver() {
						break
					}
					fenKey := engine.BookKey(g.FEN())

					// Champion evaluates best move
					bestMove, _, ok := p.ChooseMoveScored(g, tt)
					if !ok || bestMove == (game.Move{}) {
						break
					}

					uciMove := bestMove.UCI()

					// Record in book
					bookMu.Lock()
					bookMap[fenKey] = uciMove
					bookMu.Unlock()
					atomic.AddInt64(&movesRecorded, 1)

					// Apply move
					g.ApplyMove(bestMove.From, bestMove.To)
				}
				atomic.AddInt64(&gamesDone, 1)
			}
		}(w)
	}

	wg.Wait()
	close(stopProgress)

	totalElapsed := time.Since(startTime)
	fmt.Printf("\nCompleted %d self-play games in %s!\n", *numGames, totalElapsed.Truncate(time.Second))
	fmt.Printf("Total unique positions recorded: %d\n", len(bookMap))

	// Sort entries for deterministic output
	keys := make([]string, 0, len(bookMap))
	for k := range bookMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	f, err := os.Create(*outPath)
	if err != nil {
		fmt.Printf("Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	for _, k := range keys {
		fmt.Fprintf(f, "%s 0 1|%s\n", k, bookMap[k])
	}

	fmt.Printf("Successfully wrote %d positions to %s\n", len(keys), *outPath)
}
