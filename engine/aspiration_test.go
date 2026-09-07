package engine

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// What does the aspiration window cost at the depth actually played?
//
// It was written as a speed optimisation: search a narrow window around
// the previous iteration's score, and re-search on a fail. Bisecting a
// depth-6-loses-to-depth-5 regression put the whole 174 Elo of it here,
// so the question is no longer whether it is free but how much it costs
// where the engine plays.
// It is an experiment, not a test: it asserts nothing and only reports, so
// it can never go red. Left in the tree because the measurement is worth
// repeating, but behind an explicit opt-in, because 1200 games at depths 4
// to 6 cost more than ten minutes and blew the package's test timeout for
// everyone running `go test ./...`.
//
//	CHESS_SLOW_EXPERIMENTS=1 go test ./engine/ -run Aspiration -v -timeout 2h
func TestAspirationCostAtPlayingDepth(t *testing.T) {
	if os.Getenv("CHESS_SLOW_EXPERIMENTS") == "" {
		t.Skip("experiment: set CHESS_SLOW_EXPERIMENTS=1 to run")
	}
	depths := []int{4, 5, 6}
	if v := os.Getenv("CHESS_ASPIRATION_DEPTHS"); v != "" {
		depths = nil
		for _, f := range strings.Split(v, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(f))
			if err != nil {
				t.Fatal(err)
			}
			depths = append(depths, n)
		}
	}
	// A subtest per depth, so each verdict prints when it lands. As one
	// function the whole run reported nothing until the slowest depth
	// finished, which at depth 6 is the better part of an hour.
	for _, d := range depths {
		t.Run(fmt.Sprintf("depth-%d", d), func(t *testing.T) {
			off, on := Strong(d), Strong(d)
			off.Aspiration = false
			off.Name, on.Name = "no aspiration", "aspiration"
			res := PlayMatch(off, on, 400, 250)
			fmt.Printf("RESULT depth %d: aspiration off vs on: W-D-L %d-%d-%d  Elo %+d +/- %d\n",
				d, res.Wins, res.Draws, res.Losses, res.Elo(), res.EloMargin())
		})
	}
}
