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
	"os"
	"strings"
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
	nullR := flag.Int("null-reduction", 0, "challenger's null-move reduction in plies (0 = the historical 3)")
	nullScale := flag.Bool("null-scale", false, "challenger scales the null-move reduction with depth")
	refChampion := flag.String("ref-champion", "", "reference plays the champion described by this file, network included. Without it the reference is the plain hand-written evaluation, so a network is measured against the original baseline and not against whatever it is supposed to have improved on.")
	refKeepEP := flag.Bool("ref-nullmove-ep-bug", false, "reference keeps the en passant square across a null move, reproducing the bug fixed on 2026-09-06")
	noLMR := flag.Bool("no-lmr", false, "challenger disables late move reductions")
	features := flag.String("features", "", "comma-separated search features the challenger switches on: lmp, scaledlmr, rfp, nullgate, countermove, iir, see")
	refFeatures := flag.String("ref-features", "", "search features the reference switches on too, so a feature can be measured on top of another")
	scaledLMR := flag.Bool("scaled-lmr", false, "challenger scales reductions with depth and move number")
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
	// The piece-square tables' shapes are plausible but their size
	// relative to a pawn was set by hand and never checked against games.
	// One parameter, so this avoids the trap that has caught every
	// multi-term batch: guessing several weights at once.
	pstScale := flag.Float64("pst-scale", 0, "multiply every piece-square table (0 = leave alone)")
	openingPlies := flag.Int("opening-plies", 6, "random plies starting each game")
	bookPath := flag.String("book", "", "challenger plays from this opening book")
	refBookPath := flag.String("ref-book", "", "reference plays from this opening book too")
	matchOpenings := flag.String("match-openings", "", "start games from positions in this file instead of random plies")
	timeMS := flag.Int("time-ms", 0, "challenger plays to a per-move time budget instead of a fixed depth")
	tunedFile := flag.String("tuned-file", "", "challenger uses the fitted parameters in this file")
	openingOffset := flag.Int("opening-offset", 0, "shift the openings used, so chunked matches do not repeat games")
	tbPath := flag.String("tablebases", "", "challenger probes this generated endgame tablebase")
	refTBPath := flag.String("ref-tablebases", "", "reference probes it too")
	flag.Parse()
	engine.OpeningPlies = *openingPlies

	reference := full("reference (current FULL)", *depth)
	reference.Futility = *futility
	reference.NoCastle = *refNoCastle
	reference.NoRepetition = *refNoRep
	reference.KeepNullMoveEP = *refKeepEP

	// The challenger is the same configuration; whatever new feature is
	// under test is enabled here. Flags on the Player struct make the
	// comparison exact -- identical apart from the one change.
	cd := *cdepth
	if cd == 0 {
		cd = *depth
	}
	challenger := full("challenger", cd)
	challenger.Futility = *futility
	challenger.NullReduction = *nullR
	challenger.NullScale = *nullScale
	challenger.Tuned = *tuned
	challenger.NoLMR = *noLMR
	for _, f := range strings.Split(*features, ",") {
		switch strings.TrimSpace(f) {
		case "":
		case "lmp":
			challenger.LMP = true
		case "scaledlmr":
			challenger.ScaledLMR = true
		case "rfp":
			challenger.DeepRFP = true
		case "nullgate":
			challenger.NullGate = true
		case "countermove":
			challenger.Countermoves = true
		case "iir":
			challenger.IIR = true
		case "see":
			challenger.MainSEE = true
		default:
			fmt.Printf("unknown feature %q\n", f)
			return
		}
	}
	challenger.ScaledLMR = *scaledLMR
	challenger.NullMove = !*noNull
	challenger.QuiescePly = *qply
	challenger.Mobility = *mobility
	challenger.KingSafety = *kingSafety
	challenger.Extras = *extras
	challenger.Shape = *shape
	if *pstScale > 0 {
		sc := engine.DefaultPSTScale()
		for i := range sc {
			sc[i] *= *pstScale
		}
		challenger.PSTScale = &sc
	}
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
	if *matchOpenings != "" {
		lines, err := os.ReadFile(*matchOpenings)
		if err != nil {
			fmt.Println("match openings:", err)
			return
		}
		for _, line := range strings.Split(string(lines), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				// The book file format is "FEN|move"; take the position.
				if i := strings.LastIndexByte(line, '|'); i > 0 {
					line = strings.TrimSpace(line[:i])
				}
				engine.MatchOpenings = append(engine.MatchOpenings, line)
			}
		}
		fmt.Printf("starting games from %d real openings\n", len(engine.MatchOpenings))
	}
	for _, spec := range []struct {
		path string
		p    *engine.Player
		who  string
	}{{*bookPath, &challenger, "challenger"}, {*refBookPath, &reference, "reference"}} {
		if spec.path == "" {
			continue
		}
		b, err := engine.LoadBook(spec.path)
		if err != nil {
			fmt.Println("book:", err)
			return
		}
		spec.p.Book = b
		fmt.Printf("%s opening book: %d positions\n", spec.who, b.Len())
	}

	for _, spec := range []struct {
		path string
		p    *engine.Player
		who  string
	}{{*tbPath, &challenger, "challenger"}, {*refTBPath, &reference, "reference"}} {
		if spec.path == "" {
			continue
		}
		tb, err := engine.LoadTablebases(spec.path)
		if err != nil {
			fmt.Println("tablebases:", err)
			return
		}
		spec.p.Tablebases = tb
		fmt.Printf("%s tablebases: %d exact positions\n", spec.who, tb.Len())
	}

	if *tunedFile != "" {
		tf, err := engine.LoadTunedFile(*tunedFile)
		if err != nil {
			fmt.Println("tuned file:", err)
			return
		}
		tf.Apply(&challenger)
		fmt.Printf("challenger uses fitted parameters from %s\n", *tunedFile)
	}

	if *timeMS > 0 {
		challenger.TimeBudget = time.Duration(*timeMS) * time.Millisecond
		fmt.Printf("challenger plays to %d ms per move; the reference stays at depth %d\n",
			*timeMS, *depth)
	}

	// The reference is the plain hand-written evaluation unless told
	// otherwise, and that default silently invalidated a whole ladder: each
	// rung was raced against the original baseline rather than against the
	// champion that taught it, so seven deltas all measured the same
	// comparison and were then summed as if they compounded. The claimed
	// 2146 was 1955 + 191; calibration against Stockfish said 2041, and the
	// same instrument put the baseline at 1987, a real gain of about +54.
	if *refChampion != "" {
		c := engine.ReadChampion(*refChampion)
		rp, err := c.PlayerOrError()
		if err != nil {
			fmt.Println("ref-champion:", err)
			return
		}
		d := reference.Depth
		reference = rp
		reference.Depth = d
		reference.Name = "reference: " + c.Label
		fmt.Printf("reference is the champion: %s\n", c.Label)
	}

	// After -ref-champion, which replaces the reference wholesale: set
	// before it, these would be silently discarded.
	for _, f := range strings.Split(*refFeatures, ",") {
		switch strings.TrimSpace(f) {
		case "":
		case "lmp":
			reference.LMP = true
		case "scaledlmr":
			reference.ScaledLMR = true
		case "rfp":
			reference.DeepRFP = true
		case "nullgate":
			reference.NullGate = true
		case "countermove":
			reference.Countermoves = true
		case "iir":
			reference.IIR = true
		case "see":
			reference.MainSEE = true
		default:
			fmt.Printf("unknown feature %q\n", f)
			return
		}
	}

	engine.MatchOpeningOffset = *openingOffset

	res := engine.PlayMatch(challenger, reference, *games, *maxMoves)
	fmt.Printf("challenger (depth %d) vs reference (depth %d), %d games\n", cd, *depth, *games)
	fmt.Printf("  W-D-L %d-%d-%d   score %.3f\n", res.Wins, res.Draws, res.Losses, res.Score())
	fmt.Printf("  Elo gap %+d +/- %d   (%.0fs)\n", res.Elo(), res.EloMargin(), time.Since(t0).Seconds())
}
