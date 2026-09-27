package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"chess/engine"
	"chess/game"
)

// result is one audited game with its worst challenger move.
type result struct {
	r engine.GameRecord
	w worstDrop
	n int
}

func main() {
	in := flag.String("games", "", "JSONL written by gauntlet -games-out")
	sf := flag.String("judge", "/opt/homebrew/bin/stockfish", "UCI engine used as the ruler")
	depth := flag.Int("depth", 10, "judge depth per position")
	workers := flag.Int("workers", 8, "judge processes")
	threshold := flag.Float64("blunder", 1.5, "a single-move fall of at least this many pawns is a blunder")
	top := flag.Int("top", 12, "worst positions to print")
	probe := flag.String("probe", "", "champion file: search the worst positions with it at 10 ms and 200 ms to see whether depth or a blind spot is at fault")
	flag.Parse()

	f, err := os.Open(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var games []engine.GameRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r engine.GameRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err == nil {
			games = append(games, r)
		}
	}
	f.Close()

	var losses, draws, wins int
	var audited []engine.GameRecord
	for _, r := range games {
		switch outcome(r) {
		case 1:
			wins++
		case 0.5:
			draws++
			audited = append(audited, r)
		default:
			losses++
			audited = append(audited, r)
		}
	}
	fmt.Printf("%d games: %d won, %d drawn, %d lost; auditing %d with judge depth %d\n", len(games), wins, draws, losses, len(audited), *depth)

	results := make([]result, len(audited))
	var wg sync.WaitGroup
	jobs := make(chan int)
	for k := 0; k < *workers; k++ {
		judgeEngine, err := engine.NewStockfish(*sf, 20, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, "judge:", err)
			os.Exit(1)
		}
		wg.Add(1)
		go func(je *engine.UCIEngine) {
			defer wg.Done()
			defer je.Close()
			judge := func(g *game.Game) (float64, bool, bool) { return je.Evaluate(g, *depth) }
			for i := range jobs {
				evals, err := replay(audited[i], judge, *depth)
				if err != nil {
					continue
				}
				results[i] = result{r: audited[i], w: findWorstDrop(evals), n: len(evals)}
			}
		}(judgeEngine)
	}
	for i := range audited {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	byPhase, byKind, byPlyBucket := bucketCount{}, bucketCount{}, bucketCount{}
	bleeds, blunders := 0, 0
	sawIt, missedIt := 0, 0
	var worst []result
	for _, res := range results {
		if !res.w.found || res.w.drop < *threshold {
			bleeds++
			continue
		}
		blunders++
		byPhase[phase(res.w.at.material)]++
		kind := "quiet"
		if res.w.at.capture {
			kind = "capture"
		}
		if res.w.at.promo {
			kind = "promotion"
		}
		byKind[kind]++
		byPlyBucket[fmt.Sprintf("ply %3d-%3d", (res.w.ply/20)*20, (res.w.ply/20)*20+19)]++
		// Did the challenger's own score agree it was fine? A score at least
		// a pawn above the judge's after-move view means it did not see it.
		if own := res.w.at.ownScore; own == own { // not NaN
			if own-res.w.at.eval >= 1 {
				missedIt++
			} else {
				sawIt++
			}
		}
		worst = append(worst, res)
	}
	sort.Slice(worst, func(i, j int) bool { return worst[i].w.drop > worst[j].w.drop })

	fmt.Printf("\n%d games decided by one move of %.1f+ pawns, %d bled away with no such move\n", blunders, *threshold, bleeds)
	fmt.Printf("of the blunders, own score disagreed with the judge by 1+ pawn in %d, agreed in %d\n", missedIt, sawIt)
	for _, name := range []struct {
		title string
		b     bucketCount
	}{{"by phase", byPhase}, {"by move kind", byKind}, {"by ply", byPlyBucket}} {
		fmt.Printf("\n%s:\n", name.title)
		for _, k := range name.b.sortedKeys() {
			fmt.Printf("  %-14s %d\n", k, name.b[k])
		}
	}
	fmt.Printf("\nworst %d single moves (drop, ply, move, own score -> judge after):\n", *top)
	for i, res := range worst {
		if i >= *top {
			break
		}
		fmt.Printf("  %5.2f  ply %3d  %-5s  own %+.2f -> judge %+.2f  %s\n", res.w.drop, res.w.ply, res.w.at.move, res.w.at.ownScore, res.w.at.eval, res.w.at.fen)
	}
	if *probe != "" {
		probeWorst(*probe, worst, *top)
	}
}

// probeWorst replays the position before each worst move with the
// champion at two budgets. Same move at both means a blind spot the extra
// depth does not cure; a different move at 200 ms means the 10 ms search
// simply did not get there.
func probeWorst(championFile string, worst []result, top int) {
	c := engine.ReadChampion(championFile)
	p := c.Player()
	p.Book, p.Threads = nil, 1
	fmt.Printf("\nprobe with %s: move at 10 ms / 200 ms (score), blunder was:\n", championFile)
	fixed := 0
	for i, res := range worst {
		if i >= top {
			break
		}
		g, err := game.ParseFEN(res.w.at.before)
		if err != nil {
			continue
		}
		var picks [2]string
		var scores [2]float64
		for k, ms := range []int{10, 200} {
			q := p
			q.TimeBudget = time.Duration(ms) * time.Millisecond
			m, sc, ok := engine.PlayerPickScored(q, g)
			if ok {
				// The record's spelling, "q" included, or a repeated
				// queen promotion reads as a different move.
				picks[k], scores[k] = g.MoveUCI(m), sc
			}
		}
		verdict := "same move: blind spot"
		if picks[1] != res.w.at.move {
			verdict = "200 ms avoids it: depth"
			fixed++
		}
		fmt.Printf("  %-5s / %-5s (%+.2f / %+.2f)  blunder %-5s  %s\n", picks[0], picks[1], scores[0], scores[1], res.w.at.move, verdict)
	}
	fmt.Printf("  200 ms avoided %d of %d\n", fixed, min(top, len(worst)))
}
