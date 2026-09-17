package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
)

func main() {
	limit := flag.Int("limit", 0, "stop after this many positions; 0 reads the file")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: poolaudit [-limit N] pool.bin ...")
		os.Exit(1)
	}
	for _, path := range flag.Args() {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		auditOne(path, data, *limit)
	}
}

func auditOne(path string, data []byte, limit int) {
	var (
		total     int
		bands     = map[string]int{}
		phases    = map[string]int{}
		seen      = map[uint64]int{}
		perGame   = map[int32]int{}
		menCount  = map[int]int{}
		positive  int
		negative  int
		targetSum float64
		targetAbs float64
		worst     = math.Inf(-1)
		best      = math.Inf(1)
	)
	decode(data, func(r record) bool {
		total++
		t := float64(r.target)
		bands[labelBand(t)]++
		m := men(r)
		phases[phase(m)]++
		menCount[m]++
		seen[positionKey(r)]++
		perGame[r.game]++
		switch {
		case t > 0:
			positive++
		case t < 0:
			negative++
		}
		targetSum += t
		targetAbs += math.Abs(t)
		if t > worst {
			worst = t
		}
		if t < best {
			best = t
		}
		return limit == 0 || total < limit
	})
	if total == 0 {
		fmt.Printf("%s: empty\n", path)
		return
	}

	unique := len(seen)
	repeats := 0
	for _, n := range seen {
		if n > 1 {
			repeats += n - 1
		}
	}
	games := len(perGame)
	counts := make([]int, 0, games)
	for _, n := range perGame {
		counts = append(counts, n)
	}
	sort.Ints(counts)

	fmt.Printf("\n%s\n", path)
	fmt.Printf("  %d positions, %d distinct (%.1f%% repeats), %d games, %.1f positions per game\n",
		total, unique, share(repeats, total), games, float64(total)/float64(games))
	fmt.Printf("  median %d positions per game, widest %d\n", counts[len(counts)/2], counts[len(counts)-1])
	fmt.Printf("  label: mean %+.3f, mean magnitude %.3f, range %+.2f to %+.2f, %.1f%% favour the mover\n",
		targetSum/float64(total), targetAbs/float64(total), best, worst, share(positive, positive+negative))

	fmt.Printf("  by label\n")
	for _, k := range []string{"level (<0.3)", "slight (0.3-1)", "clear (1-3)", "winning (3-6)", "decisive (6+)"} {
		fmt.Printf("    %-16s %9d  %5.1f%%\n", k, bands[k], share(bands[k], total))
	}
	fmt.Printf("  by phase\n")
	for _, k := range []string{"opening", "middlegame", "endgame"} {
		fmt.Printf("    %-16s %9d  %5.1f%%\n", k, phases[k], share(phases[k], total))
	}
	fmt.Printf("  by material, men on the board\n")
	keys := make([]int, 0, len(menCount))
	for k := range menCount {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(keys)))
	for _, k := range keys {
		if share(menCount[k], total) < 2 {
			continue
		}
		fmt.Printf("    %2d men %11d  %5.1f%%  %s\n", k, menCount[k], share(menCount[k], total),
			bar(share(menCount[k], total)))
	}
}

func bar(pct float64) string {
	n := int(pct/2 + 0.5)
	s := ""
	for i := 0; i < n; i++ {
		s += "#"
	}
	return s
}
