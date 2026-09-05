// Command gauntlet measures one engine change against a fixed reference
// opponent and reports the Elo gap with a confidence margin.
//
// A full round-robin is the right tool for placing many engines on one
// scale, but it is far too slow to run after every change. For "did this
// change help?", a direct match against a fixed reference is both faster
// and sharper -- provided the two are close enough in strength that the
// score does not saturate.
package main

import (
	"flag"
	"fmt"
	"time"

	"chess/engine"
)

func full(name string, d int) engine.Player {
	return engine.Player{Name: name, Depth: d, UsePST: true, Quiescence: true,
		TTBits: 20, NullMove: true, Tapered: true, Iterative: true,
		Extensions: true, Aspiration: true, SEEPruning: true, Structure: true}
}

func main() {
	games := flag.Int("games", 40, "games in the match")
	maxMoves := flag.Int("max-moves", 250, "ply cap")
	depth := flag.Int("depth", 4, "search depth for the reference")
	cdepth := flag.Int("cdepth", 0, "challenger depth; 0 means same as -depth")
	futility := flag.Bool("futility", true, "enable futility pruning on the challenger")
	flag.Parse()

	reference := full("reference (current FULL)", *depth)

	// The challenger is the same configuration; whatever new feature is
	// under test is enabled here. Flags on the Player struct make the
	// comparison exact -- identical apart from the one change.
	cd := *cdepth
	if cd == 0 {
		cd = *depth
	}
	challenger := full("challenger", cd)
	challenger.Futility = *futility

	t0 := time.Now()
	res := engine.PlayMatch(challenger, reference, *games, *maxMoves)
	fmt.Printf("challenger (depth %d) vs reference (depth %d), %d games\n", cd, *depth, *games)
	fmt.Printf("  W-D-L %d-%d-%d   score %.3f\n", res.Wins, res.Draws, res.Losses, res.Score())
	fmt.Printf("  Elo gap %+d +/- %d   (%.0fs)\n", res.Elo(), res.EloMargin(), time.Since(t0).Seconds())
}
