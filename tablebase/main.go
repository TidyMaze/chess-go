// Command tablebase generates exact endgame tables by retrograde
// analysis and writes them to a file the engine loads at startup.
//
// Generation is minutes and only has to happen once, so it is a separate
// command rather than something the engine does on boot.
package main

import (
	"flag"
	"fmt"
	"time"

	"chess/board"
	"chess/engine"
)

func main() {
	out := flag.String("out", "tablebases.bin", "where to write the tables")
	threePieceOnly := flag.Bool("three-piece-only", false, "skip the four-piece configurations, which are much slower")
	flag.Parse()

	sets := engine.StandardTablebases()
	if *threePieceOnly {
		var small [][]board.ColoredPiece
		for _, s := range sets {
			if len(s) <= 3 {
				small = append(small, s)
			}
		}
		sets = small
	}

	fmt.Printf("generating %d configurations\n", len(sets))
	t0 := time.Now()
	tb := engine.BuildTablebases(sets, func(key string, entries int) {
		fmt.Printf("  %-10s %8d decisive positions  (%s elapsed)\n",
			key, entries, time.Since(t0).Truncate(time.Second))
	})
	if err := tb.Save(*out); err != nil {
		fmt.Println("save:", err)
		return
	}
	fmt.Printf("wrote %d exact positions to %s in %s\n",
		tb.Len(), *out, time.Since(t0).Truncate(time.Second))
}
