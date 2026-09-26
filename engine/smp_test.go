package engine

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"chess/board"
	"chess/game"
)

// Lazy SMP: several threads search the same position, staggered a ply
// apart, sharing the transposition table and nothing else. The helpers'
// moves are discarded; what they contribute is a table the main search
// finds already filled. The engine is single-threaded on a ten-core
// machine, and the whole gain of the goal, 2700 on the same-clock ladder
// from 2347, is a matter of plies reached per second.

// A table shared between threads must never hand back a torn entry: a
// key from one store and a score from another. Under the race detector
// this must also be clean.
func TestSharedTranspositionTableIsSafeUnderConcurrentUse(t *testing.T) {
	tt := NewTranspositionTable(12)
	tt.share()
	const workers, perWorker = 8, 4000
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				key := uint64(i*workers+w+1) * 0x9E3779B97F4A7C15
				score := float64(i%200) - 100
				tt.store(key, score, 3, 0, ttExact, board.White)
				// Another worker may have taken the slot with a different
				// key, in which case this misses. A hit must be our value.
				if got, ok := tt.probe(key, 3, 0, board.White, negInf, posInf); ok && got != score {
					t.Errorf("probe returned %v for a key stored with %v", got, score)
				}
			}
		}(w)
	}
	wg.Wait()
}

// Four threads on one shared table must still be exact. Every entry any
// thread stores is a correct bound for its key at its depth, so sharing
// can change how fast the root's score is found and never what it is.
func TestParallelExactSearchMatchesNaiveMinimax(t *testing.T) {
	for _, fen := range correctnessPositions {
		g, err := game.ParseFEN(fen)
		if err != nil {
			t.Fatalf("%s: %v", fen, err)
		}
		for depth := 1; depth <= 3; depth++ {
			p := exactPlayer(depth)
			_, got, ok := chooseMoveIterativeScoredThreads(g, g.Turn, depth, evalForPlayer(p), false, 0, 4)
			if !ok {
				continue
			}
			want := naiveMinimax(g, g.Turn, g.Turn, depth, 0, evalForPlayer(p))
			if math.Abs(got-want) > 1e-9 {
				t.Errorf("%s\n  depth %d: four threads %.9f, plain minimax %.9f", fen, depth, got, want)
			}
		}
	}
}

// When the move comes back the helpers are gone, and a timed search with
// helpers keeps the same clock as one without.
func TestParallelSearchStopsItsHelpersAndKeepsTheClock(t *testing.T) {
	g, err := game.ParseFEN(correctnessPositions[4])
	if err != nil {
		t.Fatal(err)
	}
	p := Strong(4)
	p.TimeBudget = 300 * time.Millisecond
	p.Threads = 4
	before := runtime.NumGoroutine()
	start := time.Now()
	if _, ok := PlayerPick(p, g); !ok {
		t.Fatal("no move")
	}
	if el := time.Since(start); el > 2*p.TimeBudget {
		t.Errorf("four threads took %v on a %v budget", el.Round(time.Millisecond), p.TimeBudget)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("%d goroutines before the search and %d after: helpers were left running", before, n)
	}
}

// More threads must search more nodes in the same time; that is the whole
// point. Timed, so it depends on the machine being free, and gated.
func TestParallelSearchSearchesMoreNodesInTheSameTime(t *testing.T) {
	if os.Getenv("MEASURE") == "" {
		t.Skip("MEASURE=1 measures the parallel speedup")
	}
	g, _ := game.ParseFEN(correctnessPositions[4])
	nodes := func(threads int) int {
		p := Strong(4)
		p.TimeBudget, p.Threads = 500*time.Millisecond, threads
		ResetNodes()
		PlayerPick(p, g)
		return LastSearchNodesValue()
	}
	one := nodes(1)
	for _, n := range []int{2, 4, 8} {
		many := nodes(n)
		t.Logf("nodes in 500ms: 1 thread %d, %d threads %d (%.2fx)", one, n, many, float64(many)/float64(one))
	}
}

// The number that matters is plies reached per second, not nodes: helpers
// that duplicate the main thread's tree add nodes and no depth. Mean
// completed depth over five positions in one second, per thread count.
func TestParallelDepthInOneSecond(t *testing.T) {
	if os.Getenv("MEASURE") == "" {
		t.Skip("MEASURE=1 measures depth reached per thread count")
	}
	net, err := LoadHalfKPNet("../champion_net.json")
	if err != nil {
		t.Skip("no champion network:", err)
	}
	// SMP_MS picks the clock; 1000 by default. Short clocks are where a
	// threading gain is smallest, since helpers only pull ahead once the
	// main thread has published a few completed depths.
	budget := time.Second
	if ms, err := strconv.Atoi(os.Getenv("SMP_MS")); err == nil && ms > 0 {
		budget = time.Duration(ms) * time.Millisecond
	}
	for _, threads := range []int{1, 2, 4, 8} {
		total := 0
		for _, fen := range correctnessPositions[3:8] {
			g, _ := game.ParseFEN(fen)
			p := Strong(4)
			p.HalfKP, p.HalfKPBlend, p.TimeBudget, p.Threads = net, 0.45, budget, threads
			PlayerPick(p, g)
			total += LastSearchDepth()
		}
		t.Logf("%d threads: mean depth %.1f in %v", threads, float64(total)/5, budget)
	}
}

// Threads travels through champion.json like everything else the UI, the
// calibrator and the ladder build a player from.
func TestChampionThreadsReachThePlayer(t *testing.T) {
	c := Champion{Depth: 4, Threads: 6}
	p, err := c.PlayerOrError()
	if err != nil {
		t.Fatal(err)
	}
	if p.Threads != 6 {
		t.Errorf("champion says threads 6, player has %d", p.Threads)
	}
	path := filepath.Join(t.TempDir(), "champion.json")
	if err := WriteChampion(path, c); err != nil {
		t.Fatal(err)
	}
	if got := ReadChampion(path).Threads; got != 6 {
		t.Errorf("threads did not survive the file: %d", got)
	}
}
