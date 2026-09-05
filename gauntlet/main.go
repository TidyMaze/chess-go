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
		TTBits: 20, NullMove: true, Tapered: true, Iterative: true}
}

func main() {
	games := flag.Int("games", 40, "games in the match")
	maxMoves := flag.Int("max-moves", 250, "ply cap")
	depth := flag.Int("depth", 4, "search depth for both sides")
	flag.Parse()

	reference := full("reference (current FULL)", *depth)

	// The challenger is the same configuration; whatever new feature is
	// under test is enabled here. Flags on the Player struct make the
	// comparison exact -- identical apart from the one change.
	challenger := full("challenger", *depth)
	challenger.Extensions = true
	challenger.Aspiration = true
	challenger.SEEPruning = true

	t0 := time.Now()
	res := engine.PlayMatch(challenger, reference, *games, *maxMoves)
	fmt.Printf("challenger vs reference (depth %d, %d games)\n", *depth, *games)
	fmt.Printf("  W-D-L %d-%d-%d   score %.3f\n", res.Wins, res.Draws, res.Losses, res.Score())
	fmt.Printf("  Elo gap %+d +/- %d   (%.0fs)\n", res.Elo(), res.EloMargin(), time.Since(t0).Seconds())
}
