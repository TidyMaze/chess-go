// Command analyze finds out what this engine actually gets wrong, by
// playing Stockfish and asking Stockfish to score every position before
// and after each of this engine's moves.
//
// Everything measured so far says "the evaluation is the ceiling", which
// is true and useless: it does not say which part. A dozen changes have
// been tried against that diagnosis and every one came back inside its
// error bars. This asks a narrower question. When the engine loses, does
// it lose in one move or in twenty? Are the losses hung pieces, or slow
// positional drift? Does it blunder in the opening, the middlegame, or
// the endgame?
//
// The answer determines what is worth building. A tactical loss pattern
// points at the search (quiescence, extensions); a drift pattern points
// at the evaluation; losses concentrated in one phase point at whatever
// is missing for that phase.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"chess/board"
	"chess/engine"
	"chess/game"
)

const stockfishPath = "/opt/homebrew/bin/stockfish"

type blunder struct {
	Ply      int     `json:"ply"`
	Phase    string  `json:"phase"`
	Move     string  `json:"move"`
	Best     string  `json:"best"`
	Loss     float64 `json:"loss"` // pawns given away by this move
	Before   float64 `json:"before"`
	After    float64 `json:"after"`
	Hanging  bool    `json:"hanging"` // the moved piece could be taken for free
	Captured bool    `json:"was_capture"`
	FEN      string  `json:"fen"`
}

// phaseName splits the game by material left, so losses can be attributed
// to a part of the game rather than a move number.
func phaseName(b *board.Board) string {
	var buf [32]board.ColoredPiece
	weight := 0
	for _, p := range b.AppendAllPieces(buf[:0]) {
		switch p.Type {
		case board.Knight, board.Bishop:
			weight += 1
		case board.Rook:
			weight += 2
		case board.Queen:
			weight += 4
		}
	}
	switch {
	case weight >= 18:
		return "opening"
	case weight >= 8:
		return "middlegame"
	}
	return "endgame"
}

// hangs reports whether the piece just moved to `sq` can be captured by
// a cheaper piece, which is the crude signature of a plain blunder as
// opposed to a positional mistake.
func hangs(g *game.Game, sq board.Sq, weights engine.Weights) bool {
	moved, ok := g.Board.PieceAt(sq)
	if !ok {
		return false
	}
	for _, m := range g.AllLegalMoves(g.Turn) {
		if m.To != sq {
			continue
		}
		attacker, ok := g.Board.PieceAt(m.From)
		if !ok {
			continue
		}
		if weights[attacker.Type] < weights[moved.Type] {
			return true
		}
		// An equal-or-greater capture still counts if nothing defends.
		undo := g.Board.MakeMove(m.From, m.To)
		defended := false
		for _, r := range g.AllLegalMoves(g.Turn.Other()) {
			if r.To == sq {
				defended = true
				break
			}
		}
		g.Board.UnmakeMove(undo)
		if !defended {
			return true
		}
	}
	return false
}

func main() {
	games := flag.Int("games", 6, "games to play and analyse")
	oppElo := flag.Int("opp-elo", 2000, "Stockfish UCI_Elo for the opponent")
	oppDepth := flag.Int("opp-depth", 6, "Stockfish search depth as opponent")
	myDepth := flag.Int("depth", 5, "this engine's depth")
	judgeDepth := flag.Int("judge-depth", 12, "Stockfish depth used to score positions")
	threshold := flag.Float64("threshold", 0.8, "pawns lost for a move to count as a blunder")
	maxPlies := flag.Int("max-plies", 200, "ply cap")
	out := flag.String("out", "analysis.json", "where to write the findings")
	flag.Parse()

	me := engine.Strong(*myDepth)
	me.Name = "me"
	weights := engine.DefaultWeights()

	opp, err := engine.NewStockfish(stockfishPath, 20, *oppElo)
	if err != nil {
		fmt.Println("stockfish:", err)
		return
	}
	defer opp.Close()
	judge, err := engine.NewStockfish(stockfishPath, 20, 0)
	if err != nil {
		fmt.Println("stockfish judge:", err)
		return
	}
	defer judge.Close()

	var all []blunder
	results := map[string]int{}
	t0 := time.Now()

	for gi := 0; gi < *games; gi++ {
		g := game.New()
		g.EnableRepetitionTracking()
		meIsWhite := gi%2 == 0

		for ply := 0; ply < *maxPlies && !g.IsOver(); ply++ {
			myTurn := (g.Turn == board.White) == meIsWhite
			if !myTurn {
				m, ok := opp.BestMove(g, *oppDepth)
				if !ok {
					break
				}
				g.ApplyMove(m.From, m.To)
				continue
			}

			// Score before, from this engine's point of view.
			before, _, okB := judge.Evaluate(g, *judgeDepth)
			if g.Turn == board.Black {
				before = -before
			}
			best, _ := judge.BestMove(g, *judgeDepth)

			m, ok := engine.PlayerPick(me, g)
			if !ok {
				break
			}
			phase := phaseName(&g.Board)
			_, wasCapture := g.Board.PieceAt(m.To)
			g.ApplyMove(m.From, m.To)

			after, _, okA := judge.Evaluate(g, *judgeDepth)
			// Evaluate reports from the side to move, which is now the
			// opponent, so flip it back to this engine's point of view.
			if g.Turn == board.White {
				after = -after
			}
			after = -after

			if okB && okA {
				loss := before - after
				if loss >= *threshold {
					all = append(all, blunder{
						Ply: ply, Phase: phase, Move: m.UCI(), Best: best.UCI(),
						Loss: loss, Before: before, After: after,
						Hanging: hangs(g, m.To, weights), Captured: wasCapture,
						FEN: g.FEN(),
					})
				}
			}
		}

		switch {
		case g.IsCheckmate(g.Turn):
			if (g.Turn == board.White) == meIsWhite {
				results["loss"]++
			} else {
				results["win"]++
			}
		default:
			results["draw"]++
		}
		fmt.Printf("game %d done (%d blunders so far, %.0fs)\n", gi+1, len(all), time.Since(t0).Seconds())
	}

	// Summarise: where and what kind.
	byPhase := map[string]int{}
	byPhaseLoss := map[string]float64{}
	hanging, huge := 0, 0
	for _, b := range all {
		byPhase[b.Phase]++
		byPhaseLoss[b.Phase] += b.Loss
		if b.Hanging {
			hanging++
		}
		if b.Loss >= 3 {
			huge++
		}
	}

	fmt.Printf("\nresults vs Stockfish %d: %v\n", *oppElo, results)
	fmt.Printf("blunders over %.1f pawns: %d\n", *threshold, len(all))
	fmt.Printf("  of which the moved piece simply hangs: %d (%.0f%%)\n",
		hanging, 100*float64(hanging)/math.Max(1, float64(len(all))))
	fmt.Printf("  of which cost 3+ pawns: %d (%.0f%%)\n",
		huge, 100*float64(huge)/math.Max(1, float64(len(all))))
	fmt.Println("\nby phase:")
	for _, p := range []string{"opening", "middlegame", "endgame"} {
		fmt.Printf("  %-11s %3d blunders, %6.1f pawns given away\n", p, byPhase[p], byPhaseLoss[p])
	}

	sort.Slice(all, func(i, j int) bool { return all[i].Loss > all[j].Loss })
	fmt.Println("\nworst moves:")
	for i, b := range all {
		if i >= 8 {
			break
		}
		tag := ""
		if b.Hanging {
			tag = "  [hangs]"
		}
		fmt.Printf("  ply %3d %-9s played %s, best %s, lost %.1f pawns%s\n",
			b.Ply, b.Phase, b.Move, b.Best, b.Loss, tag)
	}

	if data, err := json.MarshalIndent(map[string]any{
		"results": results, "blunders": all,
		"by_phase": byPhase, "by_phase_loss": byPhaseLoss,
		"hanging": hanging, "threshold": *threshold,
	}, "", "  "); err == nil {
		_ = os.WriteFile(*out, data, 0644)
	}
}
