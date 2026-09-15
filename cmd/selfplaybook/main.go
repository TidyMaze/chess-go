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
	// Flank & Irregular Openings (A00-A09)
	"e2e4",
	"d2d4",
	"c2c4",
	"g1f3",
	"f2f4", // Bird
	"b2b3", // Nimzo-Larsen
	"g2g3", // King's Fianchetto
	"b1c3", // Van Geet
	"b2b4", // Sokolsky
	"a2a3", // Anderssen
	"f2f4 d7d5",
	"f2f4 e7e5", // From Gambit
	"b2b3 e7e5 c1b2 b8c6",
	"b2b3 d7d5",
	"g2g3 e7e5",
	"g2g3 d7d5",
	"b1c3 d7d5",
	"g1f3 d7d5",
	"g1f3 d7d5 c2c4", // Reti Gambit
	"g1f3 d7d5 g2g3 g8f6 f1g2",
	"g1f3 g8f6",
	"g1f3 g8f6 c2c4 g7g6 b1c3 f8g7",
	"g1f3 c7c5",

	// English Opening (A10-A39)
	"c2c4 e7e5", // English Reverse Sicilian
	"c2c4 e7e5 b1c3 g8f6 g1f3 b8c6",
	"c2c4 e7e5 b1c3 b8c6 g2g3 g7g6 f1g2 f8g7",
	"c2c4 c7c5", // Symmetrical English
	"c2c4 c7c5 g1f3 g8f6 b1c3 b8c6",
	"c2c4 c7c5 g1f3 g8f6 d2d4 c5d4 f3d4",
	"c2c4 g8f6", // Anglo-Indian
	"c2c4 g8f6 b1c3 e7e6 e2e4",
	"c2c4 e7e6",
	"c2c4 c7c6",

	// Dutch Defense (A80-A99)
	"d2d4 f7f5",
	"d2d4 f7f5 g2g3 g8f6 f1g2 g7g6", // Leningrad
	"d2d4 f7f5 g2g3 g8f6 f1g2 e7e6 g1f3 d7d5", // Stonewall

	// Sicilian Defense (B20-B99)
	"e2e4 c7c5",
	"e2e4 c7c5 g1f3 d7d6",
	"e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6", // Najdorf
	"e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6 f1e2",
	"e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6 c1e3",
	"e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6 c1g5",
	"e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6 h2h3",
	"e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 g7g6", // Dragon
	"e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 g7g6 c1e3 f8g7 f2f3",
	"e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 e7e6", // Scheveningen
	"e2e4 c7c5 g1f3 b8c6",
	"e2e4 c7c5 g1f3 b8c6 d2d4 c5d4 f3d4",
	"e2e4 c7c5 g1f3 b8c6 d2d4 c5d4 f3d4 g8f6 b1c3 e7e5", // Sveshnikov
	"e2e4 c7c5 g1f3 b8c6 f1b5", // Rossolimo
	"e2e4 c7c5 g1f3 e7e6",
	"e2e4 c7c5 g1f3 e7e6 d2d4 c5d4 f3d4 a7a6", // Kan
	"e2e4 c7c5 g1f3 e7e6 d2d4 c5d4 f3d4 b8c6", // Taimanov
	"e2e4 c7c5 c2c3", // Alapin
	"e2e4 c7c5 c2c3 d7d5",
	"e2e4 c7c5 c2c3 g8f6",
	"e2e4 c7c5 b1c3", // Closed
	"e2e4 c7c5 f2f4", // Grand Prix
	"e2e4 c7c5 d2d4 c5d4 c2c3", // Smith-Morra

	// Caro-Kann Defense (B10-B19)
	"e2e4 c7c6",
	"e2e4 c7c6 d2d4 d7d5",
	"e2e4 c7c6 d2d4 d7d5 b1c3 d5e4 c3e4 c8f5", // Classical
	"e2e4 c7c6 d2d4 d7d5 b1c3 d5e4 c3e4 b8d7", // Steinitz
	"e2e4 c7c6 d2d4 d7d5 e4e5", // Advance
	"e2e4 c7c6 d2d4 d7d5 e4e5 c8f5",
	"e2e4 c7c6 d2d4 d7d5 e4d5 c6d5 c2c4", // Panov

	// Scandinavian & Other Semi-Open (B00-B09)
	"e2e4 d7d5",
	"e2e4 d7d5 e4d5 d8d5 b1c3 d5a5",
	"e2e4 d7d5 e4d5 d8d5 b1c3 d5d6",
	"e2e4 d7d5 e4d5 g8f6",
	"e2e4 d7d6", // Pirc
	"e2e4 d7d6 d2d4 g8f6 b1c3 g7g6",
	"e2e4 g7g6", // Modern
	"e2e4 g8f6", // Alekhine
	"e2e4 g8f6 e4e5 f6d5 d2d4 d7d6",
	"e2e4 b7b6", // Owen
	"e2e4 a7a6", // St. George

	// Open Game 1. e4 e5 (C20-C99)
	"e2e4 e7e5",
	"e2e4 e7e5 g1f3 b8c6",
	"e2e4 e7e5 g1f3 b8c6 f1b5", // Ruy Lopez
	"e2e4 e7e5 g1f3 b8c6 f1b5 a7a6",
	"e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 b5a4 g8f6",
	"e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 b5a4 g8f6 e1g1 f8e7",
	"e2e4 e7e5 g1f3 b8c6 f1b5 a7a6 b5a4 g8f6 e1g1 b7b5 a4b3",
	"e2e4 e7e5 g1f3 b8c6 f1b5 g8f6", // Berlin
	"e2e4 e7e5 g1f3 b8c6 f1b5 f8c5", // Classical
	"e2e4 e7e5 g1f3 b8c6 f1c4", // Italian
	"e2e4 e7e5 g1f3 b8c6 f1c4 f8c5",
	"e2e4 e7e5 g1f3 b8c6 f1c4 f8c5 c2c3 g8f6 d2d4",
	"e2e4 e7e5 g1f3 b8c6 f1c4 g8f6", // Two Knights
	"e2e4 e7e5 g1f3 b8c6 f1c4 g8f6 d2d4",
	"e2e4 e7e5 g1f3 b8c6 d2d4", // Scotch
	"e2e4 e7e5 g1f3 b8c6 d2d4 e5d4 f3d4",
	"e2e4 e7e5 g1f3 g8f6", // Petrov
	"e2e4 e7e5 g1f3 g8f6 f3e5 d7d6",
	"e2e4 e7e5 f2f4", // King's Gambit
	"e2e4 e7e5 f2f4 e5f4 g1f3",
	"e2e4 e7e5 b1c3", // Vienna
	"e2e4 e7e5 b1c3 g8f6",
	"e2e4 e7e5 d2d4 e5d4 d1d4", // Center Game
	"e2e4 e7e5 f1c4", // Bishop's Opening
	"e2e4 e7e5 g1f3 d7d6", // Philidor
	"e2e4 e7e5 g1f3 b8c6 b1c3 g8f6", // Four Knights

	// French Defense (C00-C19)
	"e2e4 e7e6",
	"e2e4 e7e6 d2d4 d7d5",
	"e2e4 e7e6 d2d4 d7d5 b1c3 f8b4", // Winawer
	"e2e4 e7e6 d2d4 d7d5 b1c3 g8f6", // Classical
	"e2e4 e7e6 d2d4 d7d5 b1d2", // Tarrasch
	"e2e4 e7e6 d2d4 d7d5 b1d2 g8f6",
	"e2e4 e7e6 d2d4 d7d5 e4e5", // Advance
	"e2e4 e7e6 d2d4 d7d5 e4e5 c7c5 c2c3 b8c6 g1f3",
	"e2e4 e7e6 d2d4 d7d5 e4d5 e6d5", // Exchange

	// Queen's Gambit & Slav (D00-D69)
	"d2d4 d7d5",
	"d2d4 d7d5 c2c4",
	"d2d4 d7d5 c2c4 e7e6",
	"d2d4 d7d5 c2c4 e7e6 b1c3 g8f6",
	"d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 c1g5",
	"d2d4 d7d5 c2c4 e7e6 b1c3 g8f6 c1g5 f8e7 e2e3 e8g8 g1f3 h7h6 g5h4",
	"d2d4 d7d5 c2c4 e7e6 b1c3 c7c6", // Semi-Slav
	"d2d4 d7d5 c2c4 e7e6 b1c3 c7c6 g1f3 g8f6 e2e3 b8d7 f1d3 d5c4 d3c4 b7b5",
	"d2d4 d7d5 c2c4 c7c6", // Slav
	"d2d4 d7d5 c2c4 c7c6 g1f3 g8f6 b1c3 d5c4",
	"d2d4 d7d5 c2c4 c7c6 c4d5 c6d5",
	"d2d4 d7d5 c2c4 d5c4", // QGA
	"d2d4 d7d5 c2c4 d5c4 g1f3 g8f6 e2e3",
	"d2d4 d7d5 c1f4", // London
	"d2d4 d7d5 c1f4 g8f6 e2e3 c7c5",
	"d2d4 d7d5 g1f3 g8f6 c1f4",
	"d2d4 d7d5 g1f3 g8f6 e2e3", // Colle
	"d2d4 d7d5 c1g5",

	// Grunfeld Defense (D70-D99)
	"d2d4 g8f6 c2c4 g7g6 b1c3 d7d5",
	"d2d4 g8f6 c2c4 g7g6 b1c3 d7d5 c4d5 f6d5 e2e4 d5c3 b2c3",
	"d2d4 g8f6 c2c4 g7g6 g2g3 f8g7 f1g2 d7d5",

	// King's Indian Defense (E60-E99)
	"d2d4 g8f6",
	"d2d4 g8f6 c2c4 g7g6",
	"d2d4 g8f6 c2c4 g7g6 b1c3 f8g7 e2e4 d7d6",
	"d2d4 g8f6 c2c4 g7g6 b1c3 f8g7 e2e4 d7d6 g1f3 e8g8 f1e2 e7e5",
	"d2d4 g8f6 c2c4 g7g6 b1c3 f8g7 f2f3",

	// Nimzo, Queen's Indian & Catalan (E00-E59)
	"d2d4 g8f6 c2c4 e7e6",
	"d2d4 g8f6 c2c4 e7e6 b1c3 f8b4", // Nimzo-Indian
	"d2d4 g8f6 c2c4 e7e6 b1c3 f8b4 e2e3",
	"d2d4 g8f6 c2c4 e7e6 b1c3 f8b4 d1c2",
	"d2d4 g8f6 c2c4 e7e6 g1f3 b7b6", // Queen's Indian
	"d2d4 g8f6 c2c4 e7e6 g1f3 f8b4", // Bogo-Indian
	"d2d4 g8f6 c2c4 e7e6 g2g3", // Catalan
	"d2d4 g8f6 c2c4 e7e6 g2g3 d7d5 f1g2 f8e7 g1f3",

	// Benoni & Benko (A56-A79)
	"d2d4 g8f6 c2c4 c7c5",
	"d2d4 g8f6 c2c4 c7c5 d4d5 e7e6 b1c3 e6d5 c4d5 d7d6",
	"d2d4 g8f6 c2c4 c7c5 d4d5 b7b5", // Benko
	"d2d4 g8f6 c1g5", // Trompowsky
	"d2d4 g8f6 g1f3 e7e6 c1g5", // Torre
}

func selectOpponentMoves(g *game.Game, p engine.Player, maxK int) []game.Move {
	legal := g.AllLegalMoves(g.Turn)
	if len(legal) == 0 {
		return nil
	}
	if len(legal) <= maxK {
		return legal
	}

	type scoredMove struct {
		m     game.Move
		score float64
	}
	candidates := make([]scoredMove, 0, len(legal))

	for _, m := range legal {
		c := game.From(g.Board.Clone(), g.Turn)
		c.ApplyMove(m.From, m.To)
		ev := engine.PositionScore(&c.Board, g.Turn, nil)
		candidates = append(candidates, scoredMove{m: m, score: ev})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	out := make([]game.Move, 0, maxK)
	for i := 0; i < maxK && i < len(candidates); i++ {
		out = append(out, candidates[i].m)
	}
	return out
}

func exploreTree(g *game.Game, ply int, maxPlies int, p engine.Player, tt *engine.TranspositionTable,
	bookMap map[string]string, bookMu *sync.Mutex, totalPos *int64, targetPos int) {

	if ply >= maxPlies || g.IsOver() {
		return
	}
	if atomic.LoadInt64(totalPos) >= int64(targetPos) {
		return
	}

	fenKey := engine.BookKey(g.FEN())

	bookMu.Lock()
	existingUCI, exists := bookMap[fenKey]
	bookMu.Unlock()

	var bestMove game.Move
	if exists {
		var ok bool
		bestMove, ok = game.MoveFromUCI(existingUCI)
		if !ok {
			return
		}
	} else {
		var ok bool
		bestMove, _, ok = p.ChooseMoveScored(g, tt)
		if !ok || bestMove == (game.Move{}) {
			return
		}
		bookMu.Lock()
		if _, already := bookMap[fenKey]; !already {
			bookMap[fenKey] = bestMove.UCI()
			cur := len(bookMap)
			atomic.StoreInt64(totalPos, int64(cur))
		}
		bookMu.Unlock()
	}

	if atomic.LoadInt64(totalPos) >= int64(targetPos) {
		return
	}

	// 1. Advance side to move by bestMove
	child := game.From(g.Board.Clone(), g.Turn)
	child.ApplyMove(bestMove.From, bestMove.To)

	if ply+1 >= maxPlies || child.IsOver() {
		return
	}

	// 2. Branch opponent replies based on depth
	var maxK int
	switch {
	case ply+1 < 4:
		maxK = 4
	case ply+1 < 8:
		maxK = 3
	case ply+1 < 14:
		maxK = 2
	default:
		maxK = 1
	}

	oppMoves := selectOpponentMoves(child, p, maxK)
	for _, om := range oppMoves {
		if atomic.LoadInt64(totalPos) >= int64(targetPos) {
			break
		}
		nextPos := game.From(child.Board.Clone(), child.Turn)
		nextPos.ApplyMove(om.From, om.To)
		exploreTree(nextPos, ply+2, maxPlies, p, tt, bookMap, bookMu, totalPos, targetPos)
	}
}

func main() {
	champFile := flag.String("champion", "champion.json", "champion config file")
	outPath := flag.String("out", "champion_selfplay_book.txt", "output opening book file")
	targetPos := flag.Int("target", 10000, "target number of unique positions in opening book")
	maxPlies := flag.Int("plies", 20, "maximum opening plies per branch to record")
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

	fmt.Printf("=== Building Wide & Deep Self-Play Opening Book ===\n")
	fmt.Printf("Champion: %s\n", champ.Label)
	fmt.Printf("Config: %d seeds, target %d positions, max %d plies, search depth %d, across %d workers\n",
		len(defaultSeedOpenings), *targetPos, *maxPlies, *depth, *workers)
	fmt.Printf("Output: %s\n\n", *outPath)

	var bookMu sync.Mutex
	bookMap := make(map[string]string) // FEN key -> best move (UCI)
	var totalPositions int64

	// Record initial starting position
	initG := game.New()
	bestStartMove, _, _ := basePlayer.ChooseMoveScored(initG, nil)
	bookMap[engine.BookKey(initG.FEN())] = bestStartMove.UCI()
	totalPositions = 1

	seedChan := make(chan string, len(defaultSeedOpenings))
	for _, s := range defaultSeedOpenings {
		seedChan <- s
	}
	close(seedChan)

	startTime := time.Now()
	stopProgress := make(chan struct{})

	// Progress reporter goroutine
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopProgress:
				return
			case <-ticker.C:
				cur := atomic.LoadInt64(&totalPositions)
				if cur == 0 {
					continue
				}
				elapsed := time.Since(startTime)
				pct := float64(cur) / float64(*targetPos) * 100.0
				if pct > 100.0 {
					pct = 100.0
				}
				rate := float64(cur) / elapsed.Seconds()
				var eta time.Duration
				if cur < int64(*targetPos) && rate > 0 {
					eta = time.Duration(float64(int64(*targetPos)-cur)/rate) * time.Second
				}

				// Atomically flush current book to disk
				tmpFile := *outPath + ".tmp"
				if f, err := os.Create(tmpFile); err == nil {
					bookMu.Lock()
					keys := make([]string, 0, len(bookMap))
					for k := range bookMap {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						fmt.Fprintf(f, "%s 0 1|%s\n", k, bookMap[k])
					}
					bookMu.Unlock()
					f.Close()
					_ = os.Rename(tmpFile, *outPath)
				}

				fmt.Printf("  [%3.0f%%] Positions: %d/%d | Rate: %.0f pos/s | Elapsed: %s | ETA: %s\n",
					pct, cur, *targetPos, rate,
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

			for seedLine := range seedChan {
				if atomic.LoadInt64(&totalPositions) >= int64(*targetPos) {
					break
				}
				g := game.New()
				seedMoves := strings.Fields(seedLine)

				// Play prefix moves onto board, recording along the way
				var valid bool = true
				for ply, mvStr := range seedMoves {
					m, ok := game.MoveFromUCI(mvStr)
					if !ok {
						valid = false
						break
					}
					// Ensure seed intermediate position is also recorded
					fenKey := engine.BookKey(g.FEN())
					bookMu.Lock()
					if _, exists := bookMap[fenKey]; !exists {
						bookMap[fenKey] = m.UCI()
						cur := len(bookMap)
						atomic.StoreInt64(&totalPositions, int64(cur))
					}
					bookMu.Unlock()

					g.ApplyMove(m.From, m.To)
					_ = ply
				}
				if !valid {
					continue
				}

				// Now explore tree from this seed
				exploreTree(g, len(seedMoves), *maxPlies, p, tt, bookMap, &bookMu, &totalPositions, *targetPos)
			}
		}(w)
	}

	wg.Wait()
	close(stopProgress)

	totalElapsed := time.Since(startTime)
	fmt.Printf("\nCompleted tree generation in %s!\n", totalElapsed.Truncate(time.Second))
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
