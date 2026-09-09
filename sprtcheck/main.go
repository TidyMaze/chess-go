// Command sprtcheck decides whether a running match has settled.
//
// It exists so the match driver can stop early: a feature at -70 Elo is
// decided after a hundred games and the remaining three hundred are pure
// cost. The test itself lives in engine, where it is unit tested; this is
// only a way to reach it from a shell script.
//
// Prints one word on the first line, continue, better or worse, and a
// readable summary on the second. Exit status is 0 for continue, 10 for
// better, 20 for worse, so a script can branch on it.
package main

import (
	"flag"
	"fmt"
	"os"

	"chess/engine"
)

func main() {
	w := flag.Int("w", 0, "wins")
	d := flag.Int("d", 0, "draws")
	l := flag.Int("l", 0, "losses")
	elo0 := flag.Float64("elo0", 0, "the hypothesis that the change is worth nothing")
	elo1 := flag.Float64("elo1", 15, "the hypothesis that the change is worth having")
	flag.Parse()

	s := engine.NewSPRT(*elo0, *elo1)
	s.Add(*w, *d, *l)
	lower, upper := s.Bounds()
	verdict, code := "continue", 0
	switch s.Status() {
	case engine.SPRTAcceptH1:
		verdict, code = "better", 10
	case engine.SPRTAcceptH0:
		verdict, code = "worse", 20
	}
	fmt.Println(verdict)
	fmt.Printf("  sprt[%.0f, %.0f]: %d games, llr %+.2f against bounds %.2f and %.2f -> %s\n",
		*elo0, *elo1, s.Games(), s.LLR(), lower, upper, verdict)
	os.Exit(code)
}
