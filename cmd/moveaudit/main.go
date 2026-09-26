// moveaudit asks where this engine chooses a different move from a
// stronger judge, and sorts the answers by what the disagreement costs.
//
// It exists because the engine searches twelve plies at a second a move
// and still rates about 1,985: that is not an engine at its design
// ceiling, it is one with evaluation defects, and a defect shows up as a
// move choice long before it shows up as an Elo number.
//
// The sibling command `agree` already measures how often this engine
// picks the judge's move as a rate against search depth. This one answers
// a different question: not how often we disagree but where, so it buckets
// by phase and writes out the positions themselves with what each
// disagreement costs.
//
// Stockfish is the judge and only the judge. Nothing it says is written
// to a training pool; the output is a list of positions for a human to
// read and a count per phase.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"

	"chess/engine"
	"chess/game"
)

func main() {
	champion := flag.String("champion", "champion.json", "the player under audit")
	sf := flag.String("sf", "/opt/homebrew/bin/stockfish", "the judge")
	positions := flag.Int("positions", 200, "positions to judge")
	sampleEvery := flag.Int("sample-every", 4, "judge one position in this many")
	depth := flag.Int("depth", 10, "search depth for the engine under audit")
	judgeDepth := flag.Int("judge-depth", 0, "search depth for the judge; 0 means the same as -depth. Setting them apart answers whether a disagreement is a missing evaluation term or simply less depth")
	playDepth := flag.Int("play-depth", 6, "depth the audited engine plays the games at")
	keep := flag.Int("keep", 20, "how many of the costliest disagreements to print")
	maxScore := flag.Float64("max-score", 5.0, "skip positions the judge already scores beyond this, in pawns: there every sane move keeps the result and the disagreement is taste")
	out := flag.String("out", "", "write the disagreement FENs here")
	flag.Parse()

	p, err := engine.ReadChampion(*champion).PlayerOrError()
	if err != nil {
		fmt.Println("champion:", err)
		os.Exit(1)
	}
	// The audited engine plays and is judged through the same entry point
	// the races use, so what is measured is the champion as it actually
	// plays, not a search assembled here.
	// Two tables, not one. Sharing a table means the audited search's
	// entries change what the game-playing search finds, so every arm of
	// a depth sweep walks a different set of positions and the arms
	// cannot be compared.
	playTT := engine.NewTranspositionTable(22)
	auditTT := engine.NewTranspositionTable(22)
	deepPlayer := p
	deepPlayer.Depth = *depth
	deepPlayer.TimeBudget = 0
	shallow := p
	shallow.Depth = *playDepth
	shallow.TimeBudget = 0

	judge, err := engine.NewStockfish(*sf, 20, 0)
	if err != nil {
		fmt.Println("judge:", err)
		os.Exit(1)
	}

	if *judgeDepth == 0 {
		*judgeDepth = *depth
	}
	tl := newTally(*keep)
	gap := newKindGap()
	var serr scoreError
	judged, ply := 0, 0
	g := game.New()
	for judged < *positions {
		if g.IsOver() || ply > 200 {
			g, ply = game.New(), 0
			continue
		}
		ours, _, ok := shallow.ChooseMoveScored(g, playTT)
		if !ok {
			g, ply = game.New(), 0
			continue
		}
		if ply%*sampleEvery == 0 {
			deep, ourScore, okDeep := deepPlayer.ChooseMoveScored(g, auditTT)
			theirs, score, okJudge := judge.BestMoveScored(g, *judgeDepth, 0)
			if okDeep && okJudge && !math.IsNaN(score) && worthJudging(score, *maxScore) {
				agreed := deep.UCI() == theirs.UCI()
				cost := 0.0
				if !agreed {
					// Both children searched to the same depth, so the
					// horizon is identical and only the move differs.
					childScore := func(m game.Move) (float64, bool) {
						after, err := game.ParseFEN(g.FEN())
						if err != nil {
							return 0, false
						}
						after.Apply(m)
						_, sc, ok := judge.BestMoveScored(after, *judgeDepth-1, 0)
						return sc, ok && !math.IsNaN(sc)
					}
					ourAfter, okOurs := childScore(deep)
					theirAfter, okTheirs := childScore(theirs)
					if !okOurs || !okTheirs {
						continue
					}
					cost = costOfOurMove(ourAfter, theirAfter)
				}
				if !math.IsNaN(ourScore) {
					serr.add(ourScore, score)
				}
				if !agreed {
					gap.add(g.FEN(), deep, theirs)
				}
				tl.add(disagreement{fen: g.FEN(), phase: phase(g),
					ours: deep.UCI(), theirs: theirs.UCI(), costPawn: cost}, agreed)
				judged++
				if judged%20 == 0 {
					fmt.Printf("judged %d/%d\n", judged, *positions)
				}
			}
		}
		g.ApplyMove(ours.From, ours.To)
		ply++
	}

	fmt.Print("\n", tl.report())
	fmt.Print("\n", serr.report())
	fmt.Println("\nWhat the judge plays that we do not, over the disagreements:")
	fmt.Printf("%-20s %10s %10s\n", "kind", "judge only", "ours only")
	for _, kind := range moveKinds {
		fmt.Printf("%-20s %10d %10d\n", kind, gap.judgeOnly[kind], gap.ourOnly[kind])
	}
	fmt.Printf("\nThe %d costliest disagreements:\n", len(tl.worst))
	for _, d := range tl.worst {
		fmt.Printf("  %-6.2f %-11s ours %s judge %s  %s\n", d.costPawn, d.phase, d.ours, d.theirs, d.fen)
	}
	var lines []string
	for _, d := range tl.all {
		lines = append(lines, fmt.Sprintf("%s|%s|%s|%.2f|%s", d.fen, d.ours, d.theirs, d.costPawn, d.phase))
	}
	if *out != "" {
		if err := os.WriteFile(*out, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
			fmt.Println("write:", err)
		}
	}
}
