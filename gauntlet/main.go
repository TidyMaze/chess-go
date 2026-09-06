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
	p := engine.Strong(d)
	p.Name = name
	return p
}

func main() {
	games := flag.Int("games", 40, "games in the match")
	maxMoves := flag.Int("max-moves", 250, "ply cap")
	depth := flag.Int("depth", 4, "search depth for the reference")
	cdepth := flag.Int("cdepth", 0, "challenger depth; 0 means same as -depth")
	futility := flag.Bool("futility", true, "enable futility pruning on both sides")
	tuned := flag.Bool("tuned", true, "challenger uses the Texel-tuned evaluation")
	netPath := flag.String("net", "", "challenger uses this trained network instead")
	halfkpPath := flag.String("halfkp", "", "challenger uses this HalfKP network")
	blend := flag.Float64("blend", 0, "weight on the hand evaluation when a network is used")
	refNoCastle := flag.Bool("ref-no-castle", false, "reference refuses to castle")
	refNoRep := flag.Bool("ref-no-repetition", false, "reference has no repetition detection")
	noLMR := flag.Bool("no-lmr", false, "challenger disables late move reductions")
	noNull := flag.Bool("no-null", false, "challenger disables null-move pruning")
	qply := flag.Int("qply", 0, "challenger quiescence ply cap (0 = default)")
	mobility := flag.Bool("mobility", false, "challenger adds the mobility term")
	kingSafety := flag.Float64("king-safety", 0.01, "challenger's king-danger weight (0 = off)")
	extras := flag.Bool("extras", false, "challenger adds rook-on-seventh, doubled rooks and tempo")
	// Passed pawns are weighted zero by default. An early sweep found a
	// hand-picked passed-pawn bonus harmful, because it double-counts
	// with the endgame pawn table which already rewards advancement. That
	// was measured before king safety and mobility existed, so it is
	// worth one more look at a smaller weight.
	passed := flag.Float64("passed", 0, "passed pawn bonus per rank advanced")
	shape := flag.Bool("shape", false, "challenger adds outposts, connected/backward pawns, bad bishops")
	openingPlies := flag.Int("opening-plies", 6, "random plies starting each game")
	flag.Parse()
	engine.OpeningPlies = *openingPlies

	reference := full("reference (current FULL)", *depth)
	reference.Futility = *futility
	reference.NoCastle = *refNoCastle
	reference.NoRepetition = *refNoRep

	// The challenger is the same configuration; whatever new feature is
	// under test is enabled here. Flags on the Player struct make the
	// comparison exact -- identical apart from the one change.
	cd := *cdepth
	if cd == 0 {
		cd = *depth
	}
	challenger := full("challenger", cd)
	challenger.Futility = *futility
	challenger.Tuned = *tuned
	challenger.NoLMR = *noLMR
	challenger.NullMove = !*noNull
	challenger.QuiescePly = *qply
	challenger.Mobility = *mobility
	challenger.KingSafety = *kingSafety
	challenger.Extras = *extras
	challenger.Shape = *shape
	if *passed > 0 {
		sw := engine.DefaultStructureWeights()
		sw.PassedBase = *passed
		sw.PassedPerRank = *passed / 2
		challenger.StructureW = &sw
	}
	if *halfkpPath != "" {
		n, err := engine.LoadHalfKPNet(*halfkpPath)
		if err != nil {
			fmt.Println("load halfkp:", err)
			return
		}
		challenger.HalfKP = n
		challenger.HalfKPBlend = *blend
		challenger.Tuned = false
		fmt.Printf("loaded %s (hidden %d, sigmoid=%v)\n", *halfkpPath, n.H, n.Sigmoid)
	}
	if *netPath != "" {
		n, err := engine.LoadNet(*netPath)
		if err != nil {
			fmt.Println("load net:", err)
			return
		}
		challenger.Net = n
		// A residual network was fitted as a correction to the default
		// hand-written evaluation, so the challenger must keep it.
		challenger.Tuned = false
		fmt.Printf("loaded %s (residual=%v)\n", *netPath, n.Residual)
	}

	t0 := time.Now()
	res := engine.PlayMatch(challenger, reference, *games, *maxMoves)
	fmt.Printf("challenger (depth %d) vs reference (depth %d), %d games\n", cd, *depth, *games)
	fmt.Printf("  W-D-L %d-%d-%d   score %.3f\n", res.Wins, res.Draws, res.Losses, res.Score())
	fmt.Printf("  Elo gap %+d +/- %d   (%.0fs)\n", res.Elo(), res.EloMargin(), time.Since(t0).Seconds())
}
