// moveaudit asks where this engine chooses a different move from a
// stronger judge, and sorts the answers by what the disagreement costs.
//
// It exists because the engine searches twelve plies at a second a move
// and still rates about 1,985: that is not an engine at its design
// ceiling, it is one with evaluation defects, and a defect shows up as a
// move choice long before it shows up as an Elo number.
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
	depth := flag.Int("depth", 10, "search depth for both sides")
	playDepth := flag.Int("play-depth", 6, "depth the audited engine plays the games at")
	keep := flag.Int("keep", 20, "how many of the costliest disagreements to print")
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
	tt := engine.NewTranspositionTable(22)
	deepPlayer := p
	deepPlayer.Depth = *depth
	deepPlayer.TimeBudget = 0
	shallow := p
	shallow.Depth = *playDepth
	shallow.TimeBudget = 0

	judge, err := engine.NewStockfish(*sf, 0, 0)
	if err != nil {
		fmt.Println("judge:", err)
		os.Exit(1)
	}

	tl := newTally(*keep)
	judged, ply := 0, 0
	g := game.New()
	for judged < *positions {
		if g.IsOver() || ply > 200 {
			g, ply = game.New(), 0
			continue
		}
		ours, _, ok := shallow.ChooseMoveScored(g, tt)
		if !ok {
			g, ply = game.New(), 0
			continue
		}
		if ply%*sampleEvery == 0 {
			deep, _, okDeep := deepPlayer.ChooseMoveScored(g, tt)
			theirs, score, okJudge := judge.BestMoveScored(g, *depth, 0)
			if okDeep && okJudge && !math.IsNaN(score) {
				agreed := deep.UCI() == theirs.UCI()
				cost := 0.0
				if !agreed {
					after, errFEN := game.ParseFEN(g.FEN())
					if errFEN != nil {
						continue
					}
					after.ApplyMove(deep.From, deep.To)
					_, ourScore, okAfter := judge.BestMoveScored(after, *depth, 0)
					if okAfter && !math.IsNaN(ourScore) {
						// The judge scores from the side to move, so after
						// our move its score is the opponent's view.
						cost = score + ourScore
					}
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
	fmt.Printf("\nThe %d costliest disagreements:\n", len(tl.worst))
	var lines []string
	for _, d := range tl.worst {
		fmt.Printf("  %-6.2f %-11s ours %s judge %s  %s\n", d.costPawn, d.phase, d.ours, d.theirs, d.fen)
		lines = append(lines, fmt.Sprintf("%s|%s|%s|%.2f|%s", d.fen, d.ours, d.theirs, d.costPawn, d.phase))
	}
	if *out != "" {
		if err := os.WriteFile(*out, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
			fmt.Println("write:", err)
		}
	}
}
