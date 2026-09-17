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
	combined := flag.Bool("combined", false, "also report every pool pooled together, which is what training actually sees")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: poolaudit [-limit N] pool.bin ...")
		os.Exit(1)
	}
	var all stats
	for _, path := range flag.Args() {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		st := auditOne(path, data, *limit)
		if *combined {
			all.merge(st)
		}
	}
	if *combined {
		// Game ids restart in every pool, so they are namespaced by file
		// before merging; without that two pools sharing an id would look
		// like one game with twice the positions.
		all.report("ALL POOLS COMBINED (what training sees)")
	}
}

func auditOne(path string, data []byte, limit int) stats {
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
		return stats{}
	}
	st := stats{total: total, bands: bands, phases: phases, seen: seen, menCount: menCount,
		positive: positive, negative: negative, targetSum: targetSum, targetAbs: targetAbs,
		best: best, worst: worst, perGame: map[string]int{}}
	for g, n := range perGame {
		st.perGame[fmt.Sprintf("%s#%d", path, g)] = n
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
	if pv, ok := ReadProvenance(path); ok {
		fmt.Printf("  provenance: %s positions from %s, labelled by %s",
			pv.Positions, orUnknown(pv.Source), pv.Labeller)
		if pv.LabelDepth > 0 {
			fmt.Printf(" at depth %d", pv.LabelDepth)
		}
		fmt.Printf(", %d king buckets\n", pv.Buckets)
		if pv.Note != "" {
			fmt.Printf("              %s\n", pv.Note)
		}
	} else {
		fmt.Printf("  provenance: UNKNOWN, no %s beside it\n", metaPath(path))
	}
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
	return st
}

// stats is one pool's tally, kept so several can be merged and reported as
// the single corpus a training run actually sees.
type stats struct {
	total                int
	bands, phases        map[string]int
	seen                 map[uint64]int
	menCount             map[int]int
	perGame              map[string]int
	positive, negative   int
	targetSum, targetAbs float64
	best, worst          float64
}

func (s *stats) merge(o stats) {
	if o.total == 0 {
		return
	}
	if s.total == 0 {
		s.bands, s.phases, s.seen = map[string]int{}, map[string]int{}, map[uint64]int{}
		s.menCount, s.perGame = map[int]int{}, map[string]int{}
		s.best, s.worst = math.Inf(1), math.Inf(-1)
	}
	s.total += o.total
	s.positive += o.positive
	s.negative += o.negative
	s.targetSum += o.targetSum
	s.targetAbs += o.targetAbs
	if o.best < s.best {
		s.best = o.best
	}
	if o.worst > s.worst {
		s.worst = o.worst
	}
	for k, v := range o.bands {
		s.bands[k] += v
	}
	for k, v := range o.phases {
		s.phases[k] += v
	}
	for k, v := range o.menCount {
		s.menCount[k] += v
	}
	for k, v := range o.perGame {
		s.perGame[k] += v
	}
	// Positions repeated across pools are the point of this: two pools can
	// each look varied and still be the same positions twice.
	for k, v := range o.seen {
		s.seen[k] += v
	}
}

func (s *stats) report(title string) {
	if s.total == 0 {
		return
	}
	repeats := 0
	for _, n := range s.seen {
		if n > 1 {
			repeats += n - 1
		}
	}
	fmt.Printf("\n%s\n", title)
	fmt.Printf("  %d positions, %d distinct (%.1f%% repeats), %d games, %.1f positions per game\n",
		s.total, len(s.seen), share(repeats, s.total), len(s.perGame),
		float64(s.total)/float64(len(s.perGame)))
	fmt.Printf("  label: mean %+.3f, mean magnitude %.3f, range %+.2f to %+.2f, %.1f%% favour the mover\n",
		s.targetSum/float64(s.total), s.targetAbs/float64(s.total), s.best, s.worst,
		share(s.positive, s.positive+s.negative))
	fmt.Printf("  by label\n")
	for _, k := range []string{"level (<0.3)", "slight (0.3-1)", "clear (1-3)", "winning (3-6)", "decisive (6+)"} {
		fmt.Printf("    %-16s %9d  %5.1f%%\n", k, s.bands[k], share(s.bands[k], s.total))
	}
	fmt.Printf("  by phase\n")
	for _, k := range []string{"opening", "middlegame", "endgame"} {
		fmt.Printf("    %-16s %9d  %5.1f%%  %s\n", k, s.phases[k], share(s.phases[k], s.total), bar(share(s.phases[k], s.total)))
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "an unrecorded source"
	}
	return s
}

func bar(pct float64) string {
	n := int(pct/2 + 0.5)
	s := ""
	for i := 0; i < n; i++ {
		s += "#"
	}
	return s
}
